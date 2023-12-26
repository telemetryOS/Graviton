export function up(db) {
  db.collection('test').insertOne({ name: 'two' })
}

export function down(db) {
  db.collection('test').deleteOne({ name: 'two' })
}
