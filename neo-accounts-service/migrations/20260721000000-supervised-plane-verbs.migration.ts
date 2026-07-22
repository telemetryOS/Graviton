// Supervised-plane verbs migration for neo-accounts-service (ADDITIVE, Wave 1).
//
// Grows the RBAC catalog for the supervised-plane migration WITHOUT removing any
// existing verb (later waves migrate consumers and a final wave retires the old
// *Any/*Global verbs). It $addToSet's:
//
//   * the 21 supervised<Resource>.action verbs onto EVERY "Organization Owner"
//     role (root org + partner orgs) — matching rbac.OrgOwnerPermissions, so any
//     org supervises its OWN subtree (bounded by the tree-under check); the root
//     inherits them like every other org-owner role.
//   * funnelReports.read onto the ROOT org's "Organization Owner" role (and any
//     "Staff" role) — a platform-GLOBAL staff verb (rbac.StaffPermissions),
//     root-only, split from the account-plane reports.read.
//
// It is idempotent ($addToSet never duplicates) and re-runnable. It removes NO
// verb. The root org is the stable marker { kind:"organization",
// parentId:{$exists:false} }, matching the org-tree foundation migration.

// The 21 SUPERVISED verbs added to every Organization Owner role
// (rbac.OrgOwnerPermissions supervised set). Exact strings.
const SUPERVISED_VERBS: string[] = [
  "supervisedInvoices.read",
  "supervisedInvoices.pay",
  "supervisedSubscriptions.read",
  "supervisedSubscriptions.update",
  "supervisedEntitlements.read",
  "supervisedResellerBilling.read",
  "supervisedResellerBilling.update",
  "supervisedDevicePurchases.read",
  "supervisedDevicePurchases.create",
  "supervisedDevicePurchases.update",
  "supervisedDeviceLicenses.read",
  "supervisedDeviceLicenses.extend",
  "supervisedDeviceLicenses.revoke",
  "supervisedDeviceLicenses.generateRenewals",
  "supervisedLogs.read",
  "supervisedLogRelays.read",
  "supervisedLogRelays.create",
  "supervisedLogRelays.update",
  "supervisedLogRelays.delete",
  "supervisedEvents.read",
  "supervisedDevices.read",
];

// The platform-GLOBAL staff verb, root-org only (rbac.StaffPermissions).
const ROOT_GLOBAL_VERBS: string[] = ["funnelReports.read"];

// find() returns each document's _id as a 12-element byte array in the JS
// runtime (not an ObjectId instance). toObjectId rebuilds a usable ObjectId.
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

  // --- Step 1: supervised verbs onto EVERY Organization Owner role ---
  const orgOwnerRoles = roles.find({ name: "Organization Owner" });
  roles.updateMany(
    { name: "Organization Owner" },
    { $addToSet: { permissions: { $each: SUPERVISED_VERBS } }, $set: { updatedAt: now } },
  );

  // --- Step 2: funnelReports.read onto the ROOT org's Organization Owner role ---
  const roots = accounts.find({
    kind: "organization",
    parentId: { $exists: false },
  });
  let rootHex = "(none)";
  if (roots.length > 0) {
    const rootId = toObjectId(roots[0]._id);
    rootHex = rootId.toHexString();
    roles.updateMany(
      { accountId: rootId, name: "Organization Owner" },
      { $addToSet: { permissions: { $each: ROOT_GLOBAL_VERBS } }, $set: { updatedAt: now } },
    );
  } else {
    console.log(
      "supervised-plane WARNING: no root organization found; skipping funnelReports.read",
    );
  }

  // --- Step 3: funnelReports.read onto any Staff role (staff verb catalog) ---
  roles.updateMany(
    { name: "Staff" },
    { $addToSet: { permissions: { $each: ROOT_GLOBAL_VERBS } }, $set: { updatedAt: now } },
  );

  console.log(
    "supervised-plane migration summary:" +
      " orgOwnerRolesMatched=" + orgOwnerRoles.length +
      " supervisedVerbsAdded=" + SUPERVISED_VERBS.length +
      " rootOrg=" + rootHex +
      " rootGlobalVerbsAdded=" + ROOT_GLOBAL_VERBS.length,
  );
}

// Reverse: $pull the added verbs back off the same roles. Because the migration
// is purely additive, the down is a clean removal of exactly the verbs it added.
export function down(db: Handle) {
  const accounts = db.collection("accounts");
  const roles = db.collection("roles");

  roles.updateMany(
    { name: "Organization Owner" },
    { $pull: { permissions: { $in: SUPERVISED_VERBS } } },
  );

  const roots = accounts.find({
    kind: "organization",
    parentId: { $exists: false },
  });
  if (roots.length > 0) {
    const rootId = toObjectId(roots[0]._id);
    roles.updateMany(
      { accountId: rootId, name: "Organization Owner" },
      { $pull: { permissions: { $in: ROOT_GLOBAL_VERBS } } },
    );
  }

  roles.updateMany(
    { name: "Staff" },
    { $pull: { permissions: { $in: ROOT_GLOBAL_VERBS } } },
  );

  console.log("supervised-plane down: pulled supervised + funnelReports.read verbs");
}
