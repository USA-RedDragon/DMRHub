package rules

import (
	"errors"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

func PeerShouldEgress(db *gorm.DB, peer *models.Peer, packet *models.Packet) (bool, error) {
	if peer.Egress {
		allow := false
		for _, rule := range models.ListEgressRulesForPeer(db, peer.ID) {
			if rule.SubjectIDMin <= packet.Src && rule.SubjectIDMax >= packet.Src {
				allow = rule.Allow
				if !allow {
					return allow, fmt.Errorf("rule deny %d-%d", rule.SubjectIDMin, rule.SubjectIDMax)
				}
			}
		}
		if allow {
			return allow, nil
		} else {
			return false, errors.New("no matching rule")
		}
	} else {
		return false, errors.New("egress disabled")
	}
}

func PeerShouldIngress(db *gorm.DB, peer *models.Peer, packet *models.Packet) (bool, error) {
	if peer.Ingress {
		allow := false
		for _, rule := range models.ListIngressRulesForPeer(db, peer.ID) {
			if rule.SubjectIDMin <= packet.Dst && rule.SubjectIDMax >= packet.Dst {
				allow = rule.Allow
				if !allow {
					return allow, fmt.Errorf("rule deny %d-%d", rule.SubjectIDMin, rule.SubjectIDMax)
				}
			}
		}
		if allow {
			return allow, nil
		} else {
			return false, errors.New("no matching rule")
		}
	} else {
		return false, errors.New("ingress disabled")
	}
}
