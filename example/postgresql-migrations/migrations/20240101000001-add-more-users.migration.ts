interface User {
  id: number
  name: string
  email: string
  created_at: Date
}

export function up(db: Handle) {
  const users = [
    { name: 'Bob', email: 'bob@example.com' },
    { name: 'Charlie', email: 'charlie@example.com' }
  ]

  for (const user of users) {
    db.exec(sql`
      INSERT INTO users (name, email)
      VALUES (${user.name}, ${user.email})
    `)
  }

  const allUsers = db.query<User>(sql`SELECT * FROM users ORDER BY id`)
  console.log('All users:', allUsers.length)

  const alice = db.queryOne<User>(sql`SELECT * FROM users WHERE email = ${'alice@example.com'}`)
  console.log('Found Alice:', alice?.name)
}

export function down(db: Handle) {
  db.exec(sql`DELETE FROM users WHERE email IN (${'bob@example.com'}, ${'charlie@example.com'})`)
  console.log('Removed Bob and Charlie')
}
