package hwid

import (
	"sort"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

const (
	weightAnchor = 40
	weightStrong = 15
	weightWeak   = 5
)

func weightOf(kind apiv1.SignalKind, virtual, keyFallback bool) int {
	switch kind {
	case apiv1.SignalKind_SIGNAL_KIND_PLATFORM_KEY:
		if keyFallback {
			return weightStrong
		}
		return anchorWeight(virtual)
	case apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, apiv1.SignalKind_SIGNAL_KIND_PLATFORM_UUID:
		return anchorWeight(virtual)
	case apiv1.SignalKind_SIGNAL_KIND_BOARD_SERIAL,
		apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL,
		apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID,
		apiv1.SignalKind_SIGNAL_KIND_PLATFORM_SERIAL,
		apiv1.SignalKind_SIGNAL_KIND_OS_INSTALL_ID:
		return weightStrong
	case apiv1.SignalKind_SIGNAL_KIND_MAC_ADDRESS,
		apiv1.SignalKind_SIGNAL_KIND_VOLUME_ID,
		apiv1.SignalKind_SIGNAL_KIND_GPU,
		apiv1.SignalKind_SIGNAL_KIND_CPU,
		apiv1.SignalKind_SIGNAL_KIND_MEMORY_SIZE,
		apiv1.SignalKind_SIGNAL_KIND_HOSTNAME:
		return weightWeak
	default:
		return 0
	}
}

func anchorWeight(virtual bool) int {
	if virtual {
		return weightAnchor / 2
	}
	return weightAnchor
}

func countCap(kind apiv1.SignalKind) int {
	switch kind {
	case apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, apiv1.SignalKind_SIGNAL_KIND_MAC_ADDRESS:
		return 2
	default:
		return 1
	}
}

type Resolution struct {
	MachineID     string
	ClusterID     string
	Score         int
	Kinds         int
	SameMachine   bool
	SameCluster   bool
	MergeClusters []string
}

type scored struct {
	candidate Candidate
	score     int
	kinds     int
}

func score(candidate Candidate, fanOut map[string]int, limit int, virtual, keyFallback bool) (int, int) {
	used := map[apiv1.SignalKind]int{}
	total := 0
	kinds := map[apiv1.SignalKind]struct{}{}

	matched := append([]Signal(nil), candidate.Matched...)
	sort.SliceStable(matched, func(i, j int) bool {
		return weightOf(matched[i].Kind, virtual, keyFallback) > weightOf(matched[j].Kind, virtual, keyFallback)
	})

	for _, signal := range matched {
		if limit > 0 && fanOut[signal.Digest] > limit {
			continue
		}
		weight := weightOf(signal.Kind, virtual, keyFallback)
		if weight == 0 {
			continue
		}
		if used[signal.Kind] >= countCap(signal.Kind) {
			continue
		}
		used[signal.Kind]++
		total += weight
		kinds[signal.Kind] = struct{}{}
	}
	return total, len(kinds)
}

func Resolve(candidates []Candidate, fanOut map[string]int, cfg Config, virtual, keyFallback bool) Resolution {
	ranked := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		total, kinds := score(candidate, fanOut, cfg.FanOutLimit, virtual, keyFallback)
		if total == 0 {
			continue
		}
		ranked = append(ranked, scored{candidate: candidate, score: total, kinds: kinds})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].candidate.Machine.FirstSeen.Before(ranked[j].candidate.Machine.FirstSeen)
	})

	if len(ranked) == 0 {
		return Resolution{}
	}

	best := ranked[0]
	resolution := Resolution{Score: best.score, Kinds: best.kinds}
	if best.score >= cfg.MinScore && best.kinds >= cfg.MinKinds {
		resolution.SameMachine = true
		resolution.MachineID = best.candidate.Machine.ID
		resolution.ClusterID = best.candidate.Machine.ClusterID
	} else if best.score >= cfg.ClusterScore {
		resolution.SameCluster = true
		resolution.ClusterID = best.candidate.Machine.ClusterID
	} else {
		return Resolution{Score: best.score, Kinds: best.kinds}
	}

	seen := map[string]struct{}{resolution.ClusterID: {}}
	for _, entry := range ranked[1:] {
		if entry.score < cfg.ClusterScore {
			break
		}
		cluster := entry.candidate.Machine.ClusterID
		if _, ok := seen[cluster]; ok {
			continue
		}
		seen[cluster] = struct{}{}
		resolution.MergeClusters = append(resolution.MergeClusters, cluster)
	}
	return resolution
}
