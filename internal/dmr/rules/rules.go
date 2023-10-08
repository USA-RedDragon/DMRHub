package rules

import (
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

func PeerShouldEgress(db *gorm.DB, peer *models.Peer, packet *models.Packet) bool {
	if peer.Egress {
		for _, rule := range models.ListEgressRulesForPeer(db, peer.ID) {
			if rule.SubjectIDMin <= packet.Src && rule.SubjectIDMax >= packet.Src {
				return true
			}
		}
	}
	return false
}

func PeerShouldIngress(db *gorm.DB, peer *models.Peer, packet *models.Packet) bool {
	if peer.Ingress {
		for _, rule := range models.ListIngressRulesForPeer(db, peer.ID) {
			if rule.SubjectIDMin <= packet.Dst && rule.SubjectIDMax >= packet.Dst {
				return true
			}
		}
	}
	return false
}
