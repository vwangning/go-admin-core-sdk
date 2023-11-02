package mycasbin

import (
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/log"
	"github.com/casbin/casbin/v2/model"
	"github.com/go-admin-team/go-admin-core/logger"
	sdk "github.com/go-admin-team/go-admin-core/sdk"
	"github.com/go-admin-team/go-admin-core/sdk/config"
	gormAdapter "github.com/go-admin-team/gorm-adapter/v3"
	redisWatcher "github.com/go-admin-team/redis-watcher/v2"
	"github.com/go-redis/redis/v9"
	"gorm.io/gorm"
)

// Initialize the model from a string.
var text = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && (keyMatch2(r.obj, p.obj) || keyMatch(r.obj, p.obj)) && (r.act == p.act || p.act == "*")
`

var (
	enforcer *casbin.SyncedEnforcer
	once     sync.Once
)

func Setup(db *gorm.DB, _ string) *casbin.SyncedEnforcer {
	once.Do(func() {
		Apter, err := gormAdapter.NewAdapterByDBUseTableName(db, "sys", "casbin_rule")
		if err != nil && err.Error() != "invalid DDL" {
			panic(err)
		}

		m, err := model.NewModelFromString(text)
		if err != nil {
			panic(err)
		}
		enforcer, err = casbin.NewSyncedEnforcer(m, Apter)
		if err != nil {
			panic(err)
		}
		err = enforcer.LoadPolicy()
		if err != nil {
			panic(err)
		}
		// set redis watcher if redis config is not nil
		if config.CacheConfig.Redis != nil {
			if config.CacheConfig.Redis.Addr != "" {
				// 单一 Redis 模式
				w, err := redisWatcher.NewWatcher(config.CacheConfig.Redis.Addr, redisWatcher.WatcherOptions{
					Options: redis.Options{
						Network:  "tcp",
						Password: config.CacheConfig.Redis.Password,
					},
					Channel:    "/casbin",
					IgnoreSelf: false,
				})
				if err != nil {
					panic(err)
				}

				err = w.SetUpdateCallback(updateCallback)
				if err != nil {
					panic(err)
				}
				err = enforcer.SetWatcher(w)
				if err != nil {
					panic(err)
				}
			} else if len(config.CacheConfig.Redis.Addrs) > 0 && config.CacheConfig.Redis.Addrs[0] != "" {
				// Redis Cluster 模式
				w, err := redisWatcher.NewWatcher(config.CacheConfig.Redis.Addr, redisWatcher.WatcherOptions{
					ClusterOptions: redis.ClusterOptions{
						Addrs:    config.CacheConfig.Redis.Addrs,
						Password: config.CacheConfig.Redis.Password,
					},
					Channel:    "/casbin",
					IgnoreSelf: false,
				})
				if err != nil {
					panic(err)
				}

				err = w.SetUpdateCallback(updateCallback)
				if err != nil {
					panic(err)
				}
				err = enforcer.SetWatcher(w)
				if err != nil {
					panic(err)
				}
			}
		}

		log.SetLogger(&Logger{})
		enforcer.EnableLog(true)
	})

	return enforcer
}

func updateCallback(msg string) {
	l := logger.NewHelper(sdk.Runtime.GetLogger())
	l.Infof("casbin updateCallback msg: %v", msg)
	err := enforcer.LoadPolicy()
	if err != nil {
		l.Errorf("casbin LoadPolicy err: %v", err)
	}
}
