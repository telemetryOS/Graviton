export function up(g: Handle) {
  // Move legacy session keys into the sessions: namespace, preserving TTLs.
  for (const key of g.keys('sess_*')) {
    const ttl = g.ttl(key)
    g.command('RENAME', key, `sessions:${key.slice('sess_'.length)}`)
    if (ttl > 0) {
      g.expire(`sessions:${key.slice('sess_'.length)}`, ttl)
    }
  }

  g.hSet('app:meta', 'sessions-namespaced', 'true')
}

export function down(g: Handle) {
  for (const key of g.keys('sessions:*')) {
    g.command('RENAME', key, `sess_${key.slice('sessions:'.length)}`)
  }
  g.hDel('app:meta', 'sessions-namespaced')
}
