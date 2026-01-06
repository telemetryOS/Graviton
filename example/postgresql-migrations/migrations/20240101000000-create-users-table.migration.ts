export function up(db: Handle) {
  db.exec(sql`
    CREATE TABLE users (
      id SERIAL PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT UNIQUE NOT NULL,
      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    )
  `)

  const name = 'Alice'
  const email = 'alice@example.com'

  db.exec(sql`
    INSERT INTO users (name, email)
    VALUES (${name}, ${email})
  `)

  console.log('Created users table and inserted Alice')
}

export function down(db: Handle) {
  db.exec(sql`DROP TABLE users`)
  console.log('Dropped users table')
}
