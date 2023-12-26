export function up(db) {
  db.collection('test').insertOne({ name: 'one' })
}

export function down(db) {
  db.collection('test').deleteOne({ name: 'one' })
}
