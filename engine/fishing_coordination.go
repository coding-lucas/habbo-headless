package main

import (
	"sort"
	"strconv"
	"sync"
	"time"
)

// fishingCoordinator mantém pequenas reservas locais entre os processos de
// handshake. O hotel continua sendo a fonte de verdade; as reservas apenas
// evitam que duas contas deste mesmo painel insistam no mesmo peixe.
type fishingCoordinator struct {
	mu         sync.Mutex
	targets    map[string]fishingTargetLease
	roomClaims map[string]fishingRoomClaim
}

type fishingTargetLease struct {
	SessionID string
	ExpiresAt time.Time
}

type fishingRoomClaim struct {
	Destination string
	RoomKey     string
	ExpiresAt   time.Time
}

type fishingLeaseRequest struct {
	SessionID string `json:"sessionId"`
	RoomKey   string `json:"roomKey"`
	TargetID  int    `json:"targetId"`
	LeaseMS   int    `json:"leaseMs"`
}

type fishingLeaseDecision struct {
	Reserved  bool      `json:"reserved"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type fishingReleaseRequest struct {
	SessionID string `json:"sessionId"`
	RoomKey   string `json:"roomKey"`
	TargetID  int    `json:"targetId"`
}

type fishingRoomCandidate struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Users    int    `json:"users"`
	Capacity int    `json:"capacity"`
}

type fishingRoomClaimRequest struct {
	SessionID   string                 `json:"sessionId"`
	Destination string                 `json:"destination"`
	Candidates  []fishingRoomCandidate `json:"candidates"`
}

type fishingRoomClaimDecision struct {
	RoomKey string `json:"roomKey"`
}

func newFishingCoordinator() *fishingCoordinator {
	return &fishingCoordinator{
		targets:    make(map[string]fishingTargetLease),
		roomClaims: make(map[string]fishingRoomClaim),
	}
}

func (c *fishingCoordinator) reserve(request fishingLeaseRequest) fishingLeaseDecision {
	if request.SessionID == "" || request.RoomKey == "" || request.TargetID <= 0 {
		return fishingLeaseDecision{}
	}
	lease := time.Duration(request.LeaseMS) * time.Millisecond
	if lease < 15*time.Second {
		lease = 15 * time.Second
	}
	if lease > 90*time.Second {
		lease = 90 * time.Second
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	key := request.RoomKey + "#" + strconv.Itoa(request.TargetID)
	if current, exists := c.targets[key]; exists && current.SessionID != request.SessionID {
		return fishingLeaseDecision{}
	}
	expiresAt := now.Add(lease)
	c.targets[key] = fishingTargetLease{SessionID: request.SessionID, ExpiresAt: expiresAt}
	return fishingLeaseDecision{Reserved: true, ExpiresAt: expiresAt}
}

func (c *fishingCoordinator) release(request fishingReleaseRequest) {
	if request.SessionID == "" || request.RoomKey == "" || request.TargetID <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := request.RoomKey + "#" + strconv.Itoa(request.TargetID)
	if current, exists := c.targets[key]; exists && current.SessionID == request.SessionID {
		delete(c.targets, key)
	}
}

// chooseRoom distribui contas do mesmo destino primeiro pelas salas ainda sem
// bots deste painel e, depois, pela menor ocupação informada pelo hotel.
func (c *fishingCoordinator) chooseRoom(request fishingRoomClaimRequest) fishingRoomClaimDecision {
	if request.SessionID == "" || request.Destination == "" || len(request.Candidates) == 0 {
		return fishingRoomClaimDecision{}
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	claimsByRoom := make(map[string]int)
	for sessionID, claim := range c.roomClaims {
		if sessionID != request.SessionID && claim.Destination == request.Destination {
			claimsByRoom[claim.RoomKey]++
		}
	}
	candidates := append([]fishingRoomCandidate(nil), request.Candidates...)
	sort.SliceStable(candidates, func(i, j int) bool {
		leftClaims, rightClaims := claimsByRoom[candidates[i].Key], claimsByRoom[candidates[j].Key]
		if leftClaims != rightClaims {
			return leftClaims < rightClaims
		}
		leftLoad := roomLoad(candidates[i])
		rightLoad := roomLoad(candidates[j])
		if leftLoad != rightLoad {
			return leftLoad < rightLoad
		}
		return candidates[i].Key < candidates[j].Key
	})
	chosen := candidates[0]
	c.roomClaims[request.SessionID] = fishingRoomClaim{
		Destination: request.Destination,
		RoomKey:     chosen.Key,
		ExpiresAt:   now.Add(15 * time.Minute),
	}
	return fishingRoomClaimDecision{RoomKey: chosen.Key}
}

func (c *fishingCoordinator) releaseRoom(sessionID string) {
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.roomClaims, sessionID)
}

func (c *fishingCoordinator) pruneLocked(now time.Time) {
	for key, lease := range c.targets {
		if !now.Before(lease.ExpiresAt) {
			delete(c.targets, key)
		}
	}
	for sessionID, claim := range c.roomClaims {
		if !now.Before(claim.ExpiresAt) {
			delete(c.roomClaims, sessionID)
		}
	}
}

func roomLoad(candidate fishingRoomCandidate) int {
	if candidate.Capacity <= 0 {
		return candidate.Users * 100
	}
	return candidate.Users * 100 / candidate.Capacity
}
