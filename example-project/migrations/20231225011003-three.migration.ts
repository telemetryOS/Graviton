export const name = ''

export function up(db) {
  db.collection('test').insertOne({ name: 'three' })
}

export function down(db) {
  db.collection('test').deleteOne({ name: 'three' })
}
