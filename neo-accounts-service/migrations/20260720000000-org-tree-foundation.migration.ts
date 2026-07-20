// Org-tree foundation migration for neo-accounts-service.
//
// Establishes the single TelemetryOS root organization and re-parents every
// existing account under it, so the `accounts` collection becomes one tree
// (see Accounts-Service docs/specs/2026-07-20-org-tree-emulation-design.md §1,
// §7). It also seeds the root org's two system roles ("Org Owner" with the
// 39-verb ROOT permission set, "Org Member" with organizations.read only).
//
// The migration is idempotent and re-runnable:
//   * The root org is identified by the stable marker
//     { kind: "organization", parentId: { $exists: false } } — there is exactly
//     one root. An existing root is reused; a second is never created.
//   * Roles are upserted by (accountId = ROOT_ID, name).
//   * Accounts are re-parented only when not already under the root
//     (ancestors does not yet contain ROOT_ID), so re-runs are no-ops.

// The 39 ROOT "Org Owner" permissions (OrgOwnerPermissions ∪ StaffPermissions),
// exact strings, sorted.
const ORG_OWNER_PERMISSIONS: string[] = [
  "accounts.create",
  "accounts.delete",
  "accounts.reparent",
  "accounts.suspend",
  "accounts.update",
  "applications.createGlobal",
  "applications.deleteGlobal",
  "applications.updateGlobal",
  "deviceLicenses.extend",
  "deviceLicenses.generateRenewals",
  "deviceLicenses.readAny",
  "deviceLicenses.revoke",
  "devicePurchases.create",
  "devicePurchases.readAny",
  "devicePurchases.update",
  "emulation.create",
  "events.readAny",
  "invoices.payAny",
  "invoices.readAny",
  "logs.manageRelay",
  "logs.readAny",
  "organizations.create",
  "organizations.delete",
  "organizations.read",
  "organizations.update",
  "premiumPromos.read",
  "premiumPromos.update",
  "resellerBilling.readAny",
  "resellerBilling.updateAny",
  "subscriptions.readAny",
  "subscriptions.updateAny",
  "supportedByodModels.create",
  "supportedByodModels.delete",
  "supportedByodModels.read",
  "supportedByodModels.update",
  "supportedNodeSerials.create",
  "supportedNodeSerials.delete",
  "supportedNodeSerials.read",
  "supportedNodeSerials.update",
];

const ORG_MEMBER_PERMISSIONS: string[] = ["organizations.read"];

// find() returns each document's _id as a 12-element byte array in the JS
// runtime (not an ObjectId instance). toObjectId rebuilds a usable ObjectId so
// it can be reused in filters and inserts. An ObjectId passed straight through
// (has toHexString) is returned as-is.
function toObjectId(raw: any): any {
  if (raw && typeof raw.toHexString === "function") {
    return raw;
  }
  let hex = "";
  for (let i = 0; i < raw.length; i++) {
    let h = (raw[i] & 0xff).toString(16);
    if (h.length === 1) {
      h = "0" + h;
    }
    hex += h;
  }
  return new ObjectId(hex);
}

export function up(db: Handle) {
  const now = new Date();
  const accounts = db.collection("accounts");
  const roles = db.collection("roles");

  // --- Step 1: ensure the single TelemetryOS root organization exists ---
  const existingRoots = accounts.find({
    kind: "organization",
    parentId: { $exists: false },
  });

  let rootId: any;
  let rootCreated = false;
  if (existingRoots.length > 0) {
    if (existingRoots.length > 1) {
      console.log(
        "org-tree WARNING: found " +
          existingRoots.length +
          " root organizations; reusing the first and NOT creating another",
      );
    }
    rootId = toObjectId(existingRoots[0]._id);
    console.log(
      "org-tree: reusing existing root organization _id=" + rootId.toHexString(),
    );
  } else {
    rootId = new ObjectId();
    accounts.insertOne({
      _id: rootId,
      kind: "organization",
      ancestors: [],
      name: "TelemetryOS",
      createdAt: now,
      updatedAt: now,
    });
    rootCreated = true;
    console.log(
      "org-tree: created root organization _id=" + rootId.toHexString(),
    );
  }

  // --- Step 2: seed the root org's roles, idempotent by (accountId, name) ---
  const roleSpecs = [
    { name: "Org Owner", permissions: ORG_OWNER_PERMISSIONS },
    { name: "Org Member", permissions: ORG_MEMBER_PERMISSIONS },
  ];

  let rolesUpserted = 0;
  for (let i = 0; i < roleSpecs.length; i++) {
    const spec = roleSpecs[i];
    const existing = roles.find({ accountId: rootId, name: spec.name });
    if (existing.length === 0) {
      roles.insertOne({
        _id: new ObjectId(),
        accountId: rootId,
        name: spec.name,
        permissions: spec.permissions,
        system: true,
        createdAt: now,
        updatedAt: now,
      });
    } else {
      roles.updateMany(
        { accountId: rootId, name: spec.name },
        { $set: { permissions: spec.permissions, system: true, updatedAt: now } },
      );
    }
    rolesUpserted++;
  }

  // --- Step 3: backfill every non-root account under the root org ---
  // Match accounts that are not the root and are not yet parented under it
  // (ancestors does not contain ROOT_ID). Already-parented docs are skipped, so
  // re-runs modify nothing.
  const backfillResult = accounts.updateMany(
    { _id: { $ne: rootId }, ancestors: { $ne: rootId } },
    { $set: { kind: "account", parentId: rootId, ancestors: [rootId] } },
  );
  const backfilled = backfillResult ? backfillResult.modifiedCount : 0;

  // --- Step 4: record per-step counts ---
  console.log(
    "org-tree migration summary:" +
      " rootCreated=" + rootCreated +
      " rootReused=" + (!rootCreated) +
      " rolesUpserted=" + rolesUpserted +
      " accountsBackfilled=" + backfilled,
  );
}

// Best-effort reverse: strip the tree fields from accounts parented under the
// root, delete the two seeded root roles, and delete the root organization.
// Cannot perfectly restore a pre-existing kind field on accounts that had one
// before the migration (day-one there are none), hence best-effort.
export function down(db: Handle) {
  const accounts = db.collection("accounts");
  const roles = db.collection("roles");

  const roots = accounts.find({
    kind: "organization",
    parentId: { $exists: false },
  });
  if (roots.length === 0) {
    console.log("org-tree down: no root organization found; nothing to reverse");
    return;
  }
  const rootId = toObjectId(roots[0]._id);

  const stripResult = accounts.updateMany(
    { ancestors: rootId },
    { $unset: { kind: "", parentId: "", ancestors: "" } },
  );
  const stripped = stripResult ? stripResult.modifiedCount : 0;

  roles.deleteMany({
    accountId: rootId,
    name: { $in: ["Org Owner", "Org Member"] },
    system: true,
  });

  accounts.deleteOne({ _id: rootId });

  console.log(
    "org-tree down: stripped " +
      stripped +
      " accounts, removed seeded root roles, deleted root organization " +
      rootId.toHexString(),
  );
}
