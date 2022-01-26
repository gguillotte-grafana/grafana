package leader

import (
	"context"
	"errors"

	"github.com/go-redis/redis/v8"
)

// Manager of channel leadership.
type Manager interface {
	GetOrCreateLeader(ctx context.Context, channel string, currentNodeID string, newLeadershipID string) (string, string, error)
	GetLeader(ctx context.Context, channel string) (bool, string, string, error)
	RefreshLeader(ctx context.Context, channel string, currentLeadershipID string) (bool, error)
	CleanLeader(ctx context.Context, channel string, leadershipID string) (bool, error)
}

type RedisManager struct {
	prefix            string
	redisClient       *redis.Client
	getOrCreateScript *redis.Script
	refreshScript     *redis.Script
	cleanScript       *redis.Script
}

const (
	// LeadershipEntryTTLSeconds defines expiration time for leadership entry.
	LeadershipEntryTTLSeconds = 10
)

// KEYS[1] - channel hash key
// ARGV[1] - hash key expire seconds
// ARGV[2] - current node ID
// ARGV[3] - new leadership ID if key does not exist yet
// Returns leader nodeID and current leadershipID.
const getOrCreateScriptSource = `
if redis.call('exists', KEYS[1]) ~= 0 then
	return redis.call("hmget", KEYS[1], "n", "l")
end
redis.call("hmset", KEYS[1], "n", ARGV[2], "l", ARGV[3])
redis.call("expire", KEYS[1], ARGV[1])
-- TODO: can avoid Redis call.
return redis.call("hmget", KEYS[1], "n", "l")
`

// KEYS[1] - channel hash key
// ARGV[1] - hash key expire seconds
// ARGV[2] - expected leadership ID
// Returns leader nodeID and current leadershipID.
const refreshLeaderScriptSource = `
if redis.call('exists', KEYS[1]) ~= 0 then
	if redis.call('hget', KEYS[1], "l") ~= ARGV[2] then
		return 0
	end
	redis.call("expire", KEYS[1], ARGV[1])
	return 1
end
return 0
`

// KEYS[1] - channel hash key
// ARGV[1] - leadership ID
// Cleans leadership.
const cleanLeaderScriptSource = `
if redis.call("hget", KEYS[1], "l") == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`

func NewRedisManager(prefix string, redisClient *redis.Client) *RedisManager {
	return &RedisManager{
		prefix:            prefix,
		redisClient:       redisClient,
		getOrCreateScript: redis.NewScript(getOrCreateScriptSource),
		refreshScript:     redis.NewScript(refreshLeaderScriptSource),
		cleanScript:       redis.NewScript(cleanLeaderScriptSource),
	}
}

func (m *RedisManager) getPrefixedChannel(ch string) string {
	return m.prefix + ch
}

func (m *RedisManager) GetOrCreateLeader(ctx context.Context, ch string, currentNodeID string, newLeadershipID string) (string, string, error) {
	result, err := m.getOrCreateScript.Eval(ctx, m.redisClient, []string{m.getPrefixedChannel(ch)}, LeadershipEntryTTLSeconds, currentNodeID, newLeadershipID).StringSlice()
	if err != nil {
		return "", "", err
	}
	if len(result) != 2 {
		return "", "", errors.New("malformed result")
	}
	return result[0], result[1], nil
}

func (m *RedisManager) GetLeader(ctx context.Context, ch string) (bool, string, string, error) {
	result, err := m.redisClient.HMGet(ctx, m.getPrefixedChannel(ch), "n", "l").Result()
	if err != nil {
		return false, "", "", err
	}
	if len(result) != 2 {
		return false, "", "", errors.New("malformed result")
	}
	if result[0] == nil {
		return false, "", "", nil
	}
	return true, result[0].(string), result[1].(string), nil
}

func (m *RedisManager) RefreshLeader(ctx context.Context, ch string, currentLeadershipID string) (bool, error) {
	return m.refreshScript.Eval(ctx, m.redisClient, []string{m.getPrefixedChannel(ch)}, LeadershipEntryTTLSeconds, currentLeadershipID).Bool()
}

func (m *RedisManager) CleanLeader(ctx context.Context, ch string, leadershipID string) (bool, error) {
	return m.cleanScript.Eval(ctx, m.redisClient, []string{m.getPrefixedChannel(ch)}, leadershipID).Bool()
}