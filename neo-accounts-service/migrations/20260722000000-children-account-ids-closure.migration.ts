// Forward-closure backfill migration for neo-accounts-service (ADDITIVE).
//
// Materializes each account's `childrenAccountIds` — the forward-closure DUAL of
// the existing `ancestors` chain (see Accounts-Service models/account.go). For
// every node X it sets childrenAccountIds = the ids of every node whose
// `ancestors` array contains X (i.e. X's whole transitive subtree). This is what
// the tree.Descendants seam now reads instead of scanning {ancestors: X}, so a
// supervised-plane session's accountScope resolves off the field.
//
// It is idempotent and re-runnable: each node's closure is recomputed from the
// current `ancestors` state and OVERWRITTEN with $set, so a re-run converges to
// the same value regardless of prior contents (including a null/absent field
// left by a pre-closure insert). A leaf gets an empty ARRAY (never null) so the
// live incremental $addToSet/$pull maintenance always has an array to operate on.
//
// Additive: it removes nothing and touches only the childrenAccountIds field.
// It depends on the org-tree foundation migration having established the
// `ancestors` chains; run it after that migration.

// find() returns each document's _id as a 12-element byte array in the JS
// runtime (not an ObjectId instance). toObjectId rebuilds a usable ObjectId so
// it can be reused in filters and $set values. An ObjectId passed straight
// through (has toHexString) is returned as-is.
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
  const accounts = db.collection("accounts");

  const all = accounts.find({});
  let leaves = 0;
  let populated = 0;
  for (let i = 0; i < all.length; i++) {
    const id = toObjectId(all[i]._id);

    // Every node whose ancestors chain includes this node — its whole subtree.
    const children = accounts.find({ ancestors: id });
    const childIds: any[] = [];
    for (let j = 0; j < children.length; j++) {
      childIds.push(toObjectId(children[j]._id));
    }

    accounts.updateOne(
      { _id: id },
      { $set: { childrenAccountIds: childIds } },
    );
    if (childIds.length === 0) {
      leaves++;
    } else {
      populated++;
    }
  }

  console.log(
    "children-account-ids closure backfill summary:" +
      " nodes=" + all.length +
      " populated=" + populated +
      " leaves=" + leaves,
  );
}

// Reverse: strip the materialized closure field from every account. The upward
// `ancestors` chain is untouched (owned by the org-tree foundation migration),
// so the tree stays fully described by ancestors after a down.
export function down(db: Handle) {
  const accounts = db.collection("accounts");
  accounts.updateMany({}, { $unset: { childrenAccountIds: "" } });
  console.log("children-account-ids closure down: unset childrenAccountIds on all accounts");
}
