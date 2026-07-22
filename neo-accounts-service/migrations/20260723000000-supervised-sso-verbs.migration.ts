// Supervised-SSO verbs migration for neo-accounts-service (ADDITIVE).
//
// Grows the RBAC catalog with the two supervised-plane SSO verbs WITHOUT
// removing any existing verb. It $addToSet's:
//
//   * supervisedSso.read + supervisedSso.update onto EVERY "Organization Owner"
//     role (root org + partner orgs) — matching rbac.OrgOwnerPermissions, so any
//     org supervises the SSO/SAML config of its OWN subtree (bounded by the
//     tree-under check); the root inherits them like every other org-owner role.
//
// These gate Authentication-Service's /supervised/account/:accountId/sso read +
// write, which Manager's AccountDetails SSO/SAML tab uses to reach a descendant
// account's SSO connection (replacing the dead legacy `GET account/saml`).
//
// It is idempotent ($addToSet never duplicates) and re-runnable. It removes NO
// verb. Follows the Wave 1 supervised-plane migration; there is no root-only
// verb here — supervisedSso is a plain org-owner verb held by every org.

// The SUPERVISED SSO verbs added to every Organization Owner role
// (rbac.OrgOwnerPermissions supervised set). Exact strings.
const SUPERVISED_SSO_VERBS: string[] = [
  "supervisedSso.read",
  "supervisedSso.update",
];

export function up(db: Handle) {
  const now = new Date();
  const roles = db.collection("roles");

  const orgOwnerRoles = roles.find({ name: "Organization Owner" });
  roles.updateMany(
    { name: "Organization Owner" },
    { $addToSet: { permissions: { $each: SUPERVISED_SSO_VERBS } }, $set: { updatedAt: now } },
  );

  console.log(
    "supervised-sso migration summary:" +
      " orgOwnerRolesMatched=" + orgOwnerRoles.length +
      " supervisedSsoVerbsAdded=" + SUPERVISED_SSO_VERBS.length,
  );
}

// Reverse: $pull the added verbs back off the same roles. Because the migration
// is purely additive, the down is a clean removal of exactly the verbs it added.
export function down(db: Handle) {
  const roles = db.collection("roles");

  roles.updateMany(
    { name: "Organization Owner" },
    { $pull: { permissions: { $in: SUPERVISED_SSO_VERBS } } },
  );

  console.log("supervised-sso down: pulled supervisedSso.read + supervisedSso.update verbs");
}
