export function up(db: Handle) {
  db.collection('test').insertOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe'),
    name: 'one',
    metaInfo: { size: 42, tags: ["foo", "bar"] },
    addedAt: new Date()
  })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ _id: new ObjectId('65b8077faddfba1bb64fa9fe') })
}
