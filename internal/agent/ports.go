package agent

// portLease remains owned by a process until the process has exited and every
// proxy request that captured its control URL has finished.
type portLease struct {
	apiPort  int
	p2pPort  int
	released bool
}

// portPool tracks actual port numbers rather than only paired offsets. This
// also prevents cross-range collisions such as API base 10000 and P2P base
// 10001, where adjacent offsets would otherwise both claim port 10001.
// It is protected by Server.mu.
type portPool struct {
	used map[int]struct{}
}

func (p *portPool) acquire(apiBase, p2pBase int) (*portLease, bool) {
	if apiBase < 1 || apiBase > 65535 || p2pBase < 1 || p2pBase > 65535 || apiBase == p2pBase {
		return nil, false
	}
	if p.used == nil {
		p.used = make(map[int]struct{})
	}
	maxIndex := min(65535-apiBase, 65535-p2pBase)
	for index := 0; index <= maxIndex; index++ {
		apiPort := apiBase + index
		p2pPort := p2pBase + index
		if apiPort == p2pPort {
			continue
		}
		if _, exists := p.used[apiPort]; exists {
			continue
		}
		if _, exists := p.used[p2pPort]; exists {
			continue
		}
		p.used[apiPort] = struct{}{}
		p.used[p2pPort] = struct{}{}
		return &portLease{apiPort: apiPort, p2pPort: p2pPort}, true
	}
	return nil, false
}

func (p *portPool) release(lease *portLease) bool {
	if lease == nil || lease.released {
		return false
	}
	delete(p.used, lease.apiPort)
	delete(p.used, lease.p2pPort)
	lease.released = true
	return true
}
