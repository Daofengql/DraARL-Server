package gormdb

import (
	"crypto/subtle"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNodeNotFound          = errors.New("edge node not found")
	ErrNodeDisabled          = errors.New("edge node is disabled")
	ErrNodeCredentialInvalid = errors.New("edge node credential is invalid")
)

type NodeAuthenticationResult struct {
	Accepted        bool
	IssueCredential bool
	CredentialEpoch uint32
}

// AuthenticateNode atomically validates either the current long-term
// credential hash or a still-valid one-time registration token hash. On first
// registration it consumes the token and stores only the new credential hash.
func (r *ServerRepository) AuthenticateNode(nodeID, presentedHash, issuedCredentialHash string, now time.Time) (NodeAuthenticationResult, error) {
	var outcome NodeAuthenticationResult
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var node Server
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&node).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNodeNotFound
			}
			return err
		}
		if node.Status != 1 {
			return ErrNodeDisabled
		}
		if secureHashEqual(node.NodeTokenHash, presentedHash) {
			outcome.Accepted = true
			outcome.CredentialEpoch = node.NodeCredentialEpoch
			return nil
		}
		registrationValid := node.NodeRegistrationExpiresAt != nil && !now.After(*node.NodeRegistrationExpiresAt) &&
			secureHashEqual(node.NodeRegistrationTokenHash, presentedHash)
		if !registrationValid || issuedCredentialHash == "" {
			return ErrNodeCredentialInvalid
		}
		epoch := node.NodeCredentialEpoch + 1
		updates := map[string]interface{}{
			"node_token_hash":              issuedCredentialHash,
			"node_registration_token_hash": "",
			"node_registration_expires_at": nil,
			"node_registered_at":           now,
			"node_credential_epoch":        epoch,
		}
		if err := tx.Model(&Server{}).Where("id = ?", node.ID).Updates(updates).Error; err != nil {
			return err
		}
		outcome = NodeAuthenticationResult{Accepted: true, IssueCredential: true, CredentialEpoch: epoch}
		return nil
	})
	return outcome, err
}

func secureHashEqual(left, right string) bool {
	if len(left) != 64 || len(right) != 64 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (r *ServerRepository) GetServerByNodeID(nodeID string) (*Server, error) {
	var server Server
	if err := r.db.Where("node_id = ?", nodeID).First(&server).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &server, nil
}

func (r *ServerRepository) UpdateNodeRuntime(nodeID string, fields map[string]interface{}) error {
	return r.db.Model(&Server{}).Where("node_id = ?", nodeID).Updates(fields).Error
}

func (r *ServerRepository) UpdateNodeRuntimeForSession(nodeID string, sessionID uint64, fields map[string]interface{}) (bool, error) {
	result := r.db.Model(&Server{}).
		Where("node_id = ? AND node_control_session_id = ?", nodeID, sessionID).
		Updates(fields)
	return result.RowsAffected > 0, result.Error
}

func (r *ServerRepository) MarkNodeDisconnected(nodeID string, sessionID uint64, fields map[string]interface{}) (bool, error) {
	result := r.db.Model(&Server{}).
		Where("node_id = ? AND node_control_session_id = ?", nodeID, sessionID).
		Updates(fields)
	return result.RowsAffected > 0, result.Error
}

func (r *ServerRepository) GetNodeNames(nodeIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return result, nil
	}
	var nodes []Server
	if err := r.db.Select("node_id", "display_name").Where("node_id IN ?", nodeIDs).Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.NodeID != nil {
			result[*node.NodeID] = node.DisplayName
		}
	}
	return result, nil
}

func (r *ServerRepository) ClearCurrentEntryForNode(nodeID string) ([]*Device, error) {
	return r.clearCurrentEntries("current_entry_node_id = ?", []interface{}{nodeID})
}

func (r *ServerRepository) ClearCurrentEntryForSession(nodeID string, sessionID uint64) ([]*Device, error) {
	return r.clearCurrentEntries("current_entry_node_id = ? AND current_entry_session_id = ?", []interface{}{nodeID, sessionID})
}

func (r *ServerRepository) clearCurrentEntries(where string, args []interface{}) ([]*Device, error) {
	var affected []*Device
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(where, args...).
			Find(&affected).Error; err != nil {
			return err
		}
		if len(affected) == 0 {
			return nil
		}
		ids := make([]int, 0, len(affected))
		for _, device := range affected {
			ids = append(ids, device.ID)
		}
		return tx.Model(&Device{}).Where(where, args...).Where("id IN ?", ids).Updates(map[string]interface{}{
			"current_entry_node_id":    "",
			"current_entry_session_id": 0,
			"is_online":                false,
		}).Error
	})
	return affected, err
}
