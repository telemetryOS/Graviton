export function up(db: Handle) {
  db.collection('test').insertOne({ name: 'one' })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ name: 'one' })
}
