export function up(db: Handle) {
  db.exec(sql`
    CREATE TABLE products (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      price REAL NOT NULL,
      created_at TEXT DEFAULT CURRENT_TIMESTAMP
    )
  `)

  const products = [
    { name: 'Widget', price: 9.99 },
    { name: 'Gadget', price: 19.99 },
    { name: 'Doohickey', price: 29.99 }
  ]

  for (const product of products) {
    const result = db.exec(sql`
      INSERT INTO products (name, price)
      VALUES (${product.name}, ${product.price})
    `)
    console.log(`Inserted product with ID: ${result.lastInsertId}`)
  }
}

export function down(db: Handle) {
  db.exec(sql`DROP TABLE products`)
  console.log('Dropped products table')
}
