export function up(g: Handle) {
  // Writes to two databases are freely interleaved. Each database gets its own
  // transaction, opened lazily on first use.
  const account = g.use('accounts').collection('accounts').insertOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe'),
    name: 'Acme',
  })

  g.use('devices').collection('devices').insertOne({
    name: 'lobby-screen',
    accountID: account.insertedID,
  })

  g.use('accounts').collection('accounts').updateOne(
    { _id: new ObjectId('65b8077faddfba1bb64fa9fe') },
    { $set: { deviceCount: 1 } },
  )
}

export function down(g: Handle) {
  g.use('devices').collection('devices').deleteMany({ name: 'lobby-screen' })
  g.use('accounts').collection('accounts').deleteOne({
    _id: new ObjectId('65b8077faddfba1bb64fa9fe'),
  })
}
