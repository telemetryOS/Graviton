export function up(g: Handle) {
  g.put('thumbnails/2023/a.png', 'png-bytes-a')
  g.put('thumbnails/2023/b.png', 'png-bytes-b')

  // Year-partitioned thumbnails -> flat content-addressed layout.
  for (const key of g.list('thumbnails')) {
    const name = key.split('/').pop()
    g.move(key, `thumbs/${name}`)
  }

  console.log('Flattened thumbnails into thumbs/')
}

export function down(g: Handle) {
  for (const key of g.list('thumbs')) {
    const name = key.split('/').pop()
    g.move(key, `thumbnails/2023/${name}`)
  }
}
