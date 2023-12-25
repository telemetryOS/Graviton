export const name = 'test'

export function up(db) {
  db.collection('test').insertOne({ name: 'test' })
}

export function down(db) {
  db.collection('test').deleteOne({ name: 'test' })
}
