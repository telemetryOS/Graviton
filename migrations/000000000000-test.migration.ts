export const name = 'test'

export function up(db) {
  console.log('up', db.collection('test').find({}))
}

export function down(db) {
  console.log('down')
}
