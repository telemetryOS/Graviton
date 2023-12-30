export function up(db: Handle) {
  db.collection('test').insertOne({ name: 'three' })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ name: 'three' })
}
