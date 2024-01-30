export function up(db: Handle) {
  db.collection('test').insertOne({
    _id: new ObjectId("65b8582ee09c9ef3ba6eddba"),
    name: 'three'
  })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ name: 'three' })
}
