export const name = 'test'

console.log('Hello from the migration')

export function up(db) {
  console.log('up', db.collection('test').find({}))
}

export function down(db) {
  console.log('down')
}
