																					 
																		
																			
			   

		
	   
	   

																		
 

																		   
			  

	   
							 
			   
				
			  
		   
		   
 

																								
					 
				  
				  
																			 
				 
					 
					 
																										 
					 
 

																			   
																	   
																					 
				   
					  
							 
					   
			 
 

																								
																				 
			 
																							  
				
					
  
			
		 
 

																					
										   
																						  
			
					

								  
			
  

				  
																								   
										 
																																				 
						 
						  
						  
									
  
						   
			
 

								 
																		 
			 
					 
						   
				
 

										  
								  
			
					
									
							  
  
 

																		
																		   
			
					
									
					
							  
  
 

										
								   
			
					
					   
 

																								   
						
									  
					
					 
			 
								  
									
									  
						  
	
   
			   
  
 
package session_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	voicev1  "github.com/rkrimper1/jarvis/api/pb/voice"
	"github.com/rkrimper1/jarvis/api/internal/voice/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeConfig(sessionID, userID string, tags ...string) *voicev1.StreamConfig {
	return &voicev1.StreamConfig{
		Meta: &commonv1.RequestMeta{
			SessionId: sessionID,
			UserId:    userID,
			Timestamp: timestamppb.Now(),
		},
		ContextTags: tags,
	}
}

// storeFactory is a constructor used by the shared contract test suite.
// The maxSize parameter is advisory — Redis ignores it (no in-process cap).
type storeFactory func(ttl time.Duration, maxSize int) session.Store

// ── Shared contract test suite ────────────────────────────────────────────────
// Every function below is exercised against BOTH MemoryStore and RedisStore.

func runCreateAndGet(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)

	sess := store.Create(makeConfig("sess-1", "tony"))
	if sess == nil {
		t.Fatal("Create returned nil for first session")
	}
	if sess.ID != "sess-1" {
		t.Errorf("ID = %q, want %q", sess.ID, "sess-1")
	}
	if sess.UserID != "tony" {
		t.Errorf("UserID = %q, want %q", sess.UserID, "tony")
	}
	if sess.State != session.StateIdle {
		t.Errorf("initial state = %v, want StateIdle", sess.State)
	}

	got, ok := store.Get("sess-1")
	if !ok {
		t.Fatal("Get returned false for existing session")
	}
	if got.ID != "sess-1" {
		t.Errorf("got ID = %q, want %q", got.ID, "sess-1")
	}
}

func runGetNonExistent(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	_, ok := store.Get("does-not-exist")
	if ok {
		t.Error("Get should return false for unknown session ID")
	}
}

func runContextTagsPreserved(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	sess := store.Create(makeConfig("sess-tags", "pepper", "hardware", "security"))
	if sess == nil {
		t.Fatal("Create returned nil")
	}
	if len(sess.ContextTags) != 2 {
		t.Errorf("ContextTags len = %d, want 2", len(sess.ContextTags))
	}
}

func runNLPSessionMatchesSessionID(t *testing.T, newStore storeFactory) {
	t.Helper()
										  
	store := newStore(10*time.Minute, 100)
	sess := store.Create(makeConfig("sess-nlp", "user"))
	if sess == nil {
		t.Fatal("Create returned nil")
																								
																								
   
	}
	if sess.NLPSession != "sess-nlp" {
		t.Errorf("NLPSession = %q, want %q", sess.NLPSession, "sess-nlp")
					 
																							  
	}
}

																																										

func runTouch(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-touch", "user"))

	before, _ := store.Get("sess-touch")
	t1 := before.LastActive

	time.Sleep(5 * time.Millisecond)
	store.Touch("sess-touch")

	after, _ := store.Get("sess-touch")
	if !after.LastActive.After(t1) {
		t.Error("Touch did not advance LastActive")
	}
}

func runTouchUnknown(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	store.Touch("ghost") // must not panic
}

																																								   

func runSetState(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-state", "user"))

	transitions := []session.State{
		session.StateListening,
		session.StateProcessing,
		session.StateSpeaking,
		session.StateIdle,
	}

	for _, want := range transitions {
		store.SetState("sess-state", want)
		got, ok := store.Get("sess-state")
		if !ok {
			t.Fatal("Get returned false after SetState")
		}
		if got.State != want {
			t.Errorf("state = %v, want %v", got.State, want)
		}
	}
}

																																									  

func runDelete(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-del", "user"))

	store.Delete("sess-del")
	_, ok := store.Get("sess-del")
	if ok {
		t.Error("session should not exist after Delete")
	}
}

func runDeleteUnknown(t *testing.T, newStore storeFactory) {
	t.Helper()
	store := newStore(10*time.Minute, 100)
	store.Delete("never-existed") // must not panic
}

func runConcurrentAccess(t *testing.T, newStore storeFactory) {
	t.Helper()
																					 
																										 
																																						  
								   
																									
  
 

																																							   

											   
	store := newStore(10*time.Minute, 1000)
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", n)
			store.Create(makeConfig(id, "user"))
			store.Touch(id)
			store.SetState(id, session.StateListening)
			store.SetState(id, session.StateIdle)
			store.Delete(id)
		}(i)
	}
	wg.Wait()
}

// ── MemoryStore — extra tests (memory-specific) ───────────────────────────────

func TestMemoryStore_CapacityCap(t *testing.T) {
	store := session.NewMemoryStore(10*time.Minute, 3)

	for i := range 3 {
		id := fmt.Sprintf("sess-%d", i)
		if sess := store.Create(makeConfig(id, "user")); sess == nil {
			t.Fatalf("Create returned nil for session %d (under cap)", i)
		}
	}
	overflow := store.Create(makeConfig("sess-overflow", "user"))
	if overflow != nil {
		t.Error("Create should return nil when store is at capacity")
	}
}

// ── MemoryStore contract suite ────────────────────────────────────────────────

func memFactory(ttl time.Duration, max int) session.Store {
	return session.NewMemoryStore(ttl, max)
}

func TestMemoryStore_CreateAndGet(t *testing.T)               { runCreateAndGet(t, memFactory) }
func TestMemoryStore_GetNonExistent(t *testing.T)              { runGetNonExistent(t, memFactory) }
func TestMemoryStore_ContextTagsPreserved(t *testing.T)        { runContextTagsPreserved(t, memFactory) }
func TestMemoryStore_NLPSessionMatchesSessionID(t *testing.T)  { runNLPSessionMatchesSessionID(t, memFactory) }
func TestMemoryStore_Touch(t *testing.T)                       { runTouch(t, memFactory) }
func TestMemoryStore_TouchUnknown(t *testing.T)                { runTouchUnknown(t, memFactory) }
func TestMemoryStore_SetState(t *testing.T)                    { runSetState(t, memFactory) }
func TestMemoryStore_Delete(t *testing.T)                      { runDelete(t, memFactory) }
func TestMemoryStore_DeleteUnknown(t *testing.T)               { runDeleteUnknown(t, memFactory) }
func TestMemoryStore_ConcurrentAccess(t *testing.T)            { runConcurrentAccess(t, memFactory) }

// ── RedisStore contract suite ─────────────────────────────────────────────────
// Skipped automatically when REDIS_ADDR env is not set.
//
// To run locally:
//   docker run --rm -p 6379:6379 redis:7-alpine
//   REDIS_ADDR=localhost:6379 go test ./internal/session/... -v -run Redis

func redisFactoryOrSkip(t *testing.T) storeFactory {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set — skipping Redis integration tests")
	}
	return func(ttl time.Duration, _ int) session.Store {
		store, err := session.NewRedisStore(addr, "", 1, ttl) // DB 1 = test isolation
		if err != nil {
			t.Fatalf("NewRedisStore(%s): %v", addr, err)
		}
		return store
	}
}

func TestRedisStore_CreateAndGet(t *testing.T)               { runCreateAndGet(t, redisFactoryOrSkip(t)) }
func TestRedisStore_GetNonExistent(t *testing.T)              { runGetNonExistent(t, redisFactoryOrSkip(t)) }
func TestRedisStore_ContextTagsPreserved(t *testing.T)        { runContextTagsPreserved(t, redisFactoryOrSkip(t)) }
func TestRedisStore_NLPSessionMatchesSessionID(t *testing.T)  { runNLPSessionMatchesSessionID(t, redisFactoryOrSkip(t)) }
func TestRedisStore_Touch(t *testing.T)                       { runTouch(t, redisFactoryOrSkip(t)) }
func TestRedisStore_TouchUnknown(t *testing.T)                { runTouchUnknown(t, redisFactoryOrSkip(t)) }
func TestRedisStore_SetState(t *testing.T)                    { runSetState(t, redisFactoryOrSkip(t)) }
func TestRedisStore_Delete(t *testing.T)                      { runDelete(t, redisFactoryOrSkip(t)) }
func TestRedisStore_DeleteUnknown(t *testing.T)               { runDeleteUnknown(t, redisFactoryOrSkip(t)) }
func TestRedisStore_ConcurrentAccess(t *testing.T)            { runConcurrentAccess(t, redisFactoryOrSkip(t)) }

// ── NewStore factory ──────────────────────────────────────────────────────────

func TestNewStore_MemoryProvider(t *testing.T) {
	store, err := session.NewStore(session.StoreConfig{
		Provider: "memory",
		TTL:      5 * time.Minute,
		MaxSize:  100,
	})
	if err != nil {
		t.Fatalf("NewStore memory: %v", err)
	}
	// Verify the returned store is functional.
	store.Create(makeConfig("factory-mem", "user"))
	_, ok := store.Get("factory-mem")
	if !ok {
		t.Error("MemoryStore created via factory is not functional")
	}
}

func TestNewStore_UnknownProviderDefaultsToMemory(t *testing.T) {
	store, err := session.NewStore(session.StoreConfig{
		Provider: "unknown-backend",
		TTL:      5 * time.Minute,
		MaxSize:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error for unknown provider: %v", err)
	}
	if store == nil {
		t.Fatal("expected a store, got nil")
	}
}

func TestNewStore_Redis_FailsOnBadAddr(t *testing.T) {
	_, err := session.NewStore(session.StoreConfig{
		Provider:  "redis",
		TTL:       5 * time.Minute,
		RedisAddr: "localhost:19999", // nothing listening here
	})
	if err == nil {
		t.Error("expected error for unreachable Redis, got nil")
	}
}

func TestNewStore_Redis_HappyPath(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set — skipping Redis factory happy-path test")
	}
	store, err := session.NewStore(session.StoreConfig{
		Provider:  "redis",
		TTL:       5 * time.Minute,
		RedisAddr: addr,
		RedisDB:   1,
	})
	if err != nil {
		t.Fatalf("NewStore redis: %v", err)
	}
	store.Create(makeConfig("factory-redis", "user"))
	_, ok := store.Get("factory-redis")
	if !ok {
		t.Error("RedisStore created via factory is not functional")
	}
}