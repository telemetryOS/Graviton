export function up(g: Handle) {
  g.write('assets/logos/acme.svg', '<svg>acme</svg>')
  g.write('assets/logos/globex.svg', '<svg>globex</svg>')

  // Flat legacy layout -> per-kind folders.
  for (const entry of g.list('assets/logos')) {
    if (!entry.isDir) {
      g.move(`assets/logos/${entry.name}`, `assets/images/logos/${entry.name}`)
    }
  }

  console.log('Moved logos into assets/images/logos')
}

export function down(g: Handle) {
  for (const entry of g.list('assets/images/logos')) {
    if (!entry.isDir) {
      g.move(`assets/images/logos/${entry.name}`, `assets/logos/${entry.name}`)
    }
  }
  g.removeAll('assets/images')
}
