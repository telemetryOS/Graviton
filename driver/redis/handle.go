package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Handle is the migration-facing handle bound to a single redis database.
// Operations apply immediately (no transaction; see the package comment) and
// panic on error like every other driver's handle. Missing-key reads return
// null rather than panicking, matching findOne()'s no-match behavior.
type Handle struct {
	ctx    context.Context
	driver *Driver
}

// Get returns the string value at key, or null when the key does not exist.
func (h *Handle) Get(key string) any {
	value, err := h.driver.client.Get(h.ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return value
}

// Set stores value at key. An optional trailing argument is a TTL in seconds
// (0 or omitted = no expiry).
func (h *Handle) Set(key string, value any, ttlSeconds ...int64) {
	var ttl time.Duration
	if len(ttlSeconds) > 0 {
		ttl = time.Duration(ttlSeconds[0]) * time.Second
	}
	if err := h.driver.client.Set(h.ctx, key, value, ttl).Err(); err != nil {
		panic(err)
	}
}

// Del deletes keys and returns how many existed.
func (h *Handle) Del(keys ...string) int64 {
	deleted, err := h.driver.client.Del(h.ctx, keys...).Result()
	if err != nil {
		panic(err)
	}
	return deleted
}

// Keys returns the keys matching a glob pattern. It iterates with SCAN rather
// than KEYS so a migration against a large production store never blocks the
// server; SCAN may report a key more than once across rehashes, so results are
// deduplicated.
func (h *Handle) Keys(pattern string) []string {
	seen := make(map[string]bool)
	keys := []string{}
	iter := h.driver.client.Scan(h.ctx, 0, pattern, 0).Iterator()
	for iter.Next(h.ctx) {
		key := iter.Val()
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if err := iter.Err(); err != nil {
		panic(err)
	}
	return keys
}

// Exists reports whether key exists.
func (h *Handle) Exists(key string) bool {
	n, err := h.driver.client.Exists(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	return n > 0
}

// Expire sets key's TTL in seconds, reporting whether the key existed.
func (h *Handle) Expire(key string, seconds int64) bool {
	ok, err := h.driver.client.Expire(h.ctx, key, time.Duration(seconds)*time.Second).Result()
	if err != nil {
		panic(err)
	}
	return ok
}

// Ttl returns key's remaining TTL in seconds: -1 when the key has no expiry,
// -2 when it does not exist.
func (h *Handle) Ttl(key string) int64 {
	ttl, err := h.driver.client.TTL(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	if ttl < 0 {
		return int64(ttl)
	}
	return int64(ttl / time.Second)
}

// HGet returns the hash field's value, or null when the key or field does not
// exist.
func (h *Handle) HGet(key string, field string) any {
	value, err := h.driver.client.HGet(h.ctx, key, field).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return value
}

// HSet stores a hash field.
func (h *Handle) HSet(key string, field string, value any) {
	if err := h.driver.client.HSet(h.ctx, key, field, value).Err(); err != nil {
		panic(err)
	}
}

// HGetAll returns the whole hash as an object; a missing key is an empty
// object.
func (h *Handle) HGetAll(key string) map[string]string {
	values, err := h.driver.client.HGetAll(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	return values
}

// HDel deletes hash fields and returns how many existed.
func (h *Handle) HDel(key string, fields ...string) int64 {
	deleted, err := h.driver.client.HDel(h.ctx, key, fields...).Result()
	if err != nil {
		panic(err)
	}
	return deleted
}

// SAdd adds members to a set and returns how many were newly added.
func (h *Handle) SAdd(key string, members ...any) int64 {
	added, err := h.driver.client.SAdd(h.ctx, key, members...).Result()
	if err != nil {
		panic(err)
	}
	return added
}

// SRem removes members from a set and returns how many were removed.
func (h *Handle) SRem(key string, members ...any) int64 {
	removed, err := h.driver.client.SRem(h.ctx, key, members...).Result()
	if err != nil {
		panic(err)
	}
	return removed
}

// SMembers returns every member of a set; a missing key is an empty array.
func (h *Handle) SMembers(key string) []string {
	members, err := h.driver.client.SMembers(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	return members
}

// SIsMember reports whether member is in the set.
func (h *Handle) SIsMember(key string, member any) bool {
	isMember, err := h.driver.client.SIsMember(h.ctx, key, member).Result()
	if err != nil {
		panic(err)
	}
	return isMember
}

// LPush prepends values to a list and returns the list's new length.
func (h *Handle) LPush(key string, values ...any) int64 {
	length, err := h.driver.client.LPush(h.ctx, key, values...).Result()
	if err != nil {
		panic(err)
	}
	return length
}

// RPush appends values to a list and returns the list's new length.
func (h *Handle) RPush(key string, values ...any) int64 {
	length, err := h.driver.client.RPush(h.ctx, key, values...).Result()
	if err != nil {
		panic(err)
	}
	return length
}

// LRange returns the list elements between start and stop inclusive
// (0, -1 returns the whole list); a missing key is an empty array.
func (h *Handle) LRange(key string, start int64, stop int64) []string {
	values, err := h.driver.client.LRange(h.ctx, key, start, stop).Result()
	if err != nil {
		panic(err)
	}
	return values
}

// LLen returns the list's length; a missing key is 0.
func (h *Handle) LLen(key string) int64 {
	length, err := h.driver.client.LLen(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	return length
}

// ZAdd adds (or updates) one scored member in a sorted set and returns whether
// the member was newly added.
func (h *Handle) ZAdd(key string, score float64, member string) bool {
	added, err := h.driver.client.ZAdd(h.ctx, key, goredis.Z{Score: score, Member: member}).Result()
	if err != nil {
		panic(err)
	}
	return added == 1
}

// ZRem removes members from a sorted set and returns how many were removed.
func (h *Handle) ZRem(key string, members ...any) int64 {
	removed, err := h.driver.client.ZRem(h.ctx, key, members...).Result()
	if err != nil {
		panic(err)
	}
	return removed
}

// ZRange returns the members ranked between start and stop inclusive, lowest
// score first (0, -1 returns the whole set); a missing key is an empty array.
func (h *Handle) ZRange(key string, start int64, stop int64) []string {
	members, err := h.driver.client.ZRange(h.ctx, key, start, stop).Result()
	if err != nil {
		panic(err)
	}
	return members
}

// ZScore returns member's score, or null when the key or member is missing.
func (h *Handle) ZScore(key string, member string) any {
	score, err := h.driver.client.ZScore(h.ctx, key, member).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return score
}

// Incr increments the integer at key by one (creating it at 0 first) and
// returns the new value.
func (h *Handle) Incr(key string) int64 {
	value, err := h.driver.client.Incr(h.ctx, key).Result()
	if err != nil {
		panic(err)
	}
	return value
}

// IncrBy increments the integer at key by delta (creating it at 0 first) and
// returns the new value.
func (h *Handle) IncrBy(key string, delta int64) int64 {
	value, err := h.driver.client.IncrBy(h.ctx, key, delta).Result()
	if err != nil {
		panic(err)
	}
	return value
}

// Command runs any Redis command verbatim (e.g. command('RENAME', 'a', 'b') or
// command('SETBIT', 'flags', 7, 1)) and returns the server's reply. A nil
// reply (e.g. GET on a missing key) returns null.
func (h *Handle) Command(args ...any) any {
	reply, err := h.driver.client.Do(h.ctx, args...).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return reply
}
