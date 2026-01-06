interface Product {
  id: number
  name: string
  price: number
  created_at: string
}

export function up(db: Handle) {
  const products = db.query<Product>(sql`SELECT * FROM products WHERE price > ${15.0} ORDER BY price`)
  console.log(`Found ${products.length} products over $15`)

  const widget = db.queryOne<Product>(sql`SELECT * FROM products WHERE name = ${'Widget'}`)
  if (widget) {
    console.log(`Widget costs $${widget.price}`)
  }
}

export function down(db: Handle) {
  console.log('No changes to revert')
}
