export function up(db: Handle) {
  db.collection('test').insertOne({
    _id: new ObjectId("65b8582ee09c9ef3ba6eddb9"),
    name: 'two'
  })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ _id: new ObjectId("65b8582ee09c9ef3ba6eddb9") })
}
