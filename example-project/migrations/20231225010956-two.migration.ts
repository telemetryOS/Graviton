export function up(db: Handle) {
  db.collection('test').insertOne({ name: 'two' })
}

export function down(db: Handle) {
  db.collection('test').deleteOne({ name: 'two' })
}
