package gormdb

import (
	"gorm.io/gorm"
	"time"
)

// OperatorCertRepository 操作证仓储
type OperatorCertRepository struct {
	db *gorm.DB
}

// NewOperatorCertRepository 创建操作证仓储
func NewOperatorCertRepository() *OperatorCertRepository {
	return &OperatorCertRepository{db: Get()}
}

// Create 创建操作证记录
func (r *OperatorCertRepository) Create(cert *OperatorCert) error {
	return r.db.Create(cert).Error
}

// GetByUserID 获取用户的操作证（兼容旧接口，返回任意一条）
func (r *OperatorCertRepository) GetByUserID(userID int) (*OperatorCert, error) {
	return r.GetActiveByUserID(userID)
}

// GetActiveByUserID 获取用户当前有效的操作证（status=1）
func (r *OperatorCertRepository) GetActiveByUserID(userID int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.Where("user_id = ? AND status = ?", userID, 1).First(&cert).Error
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// GetLatestByUserID 获取用户最新的操作证（不管状态）
// 用于显示最新的上传状态（待审核、已拒绝等）
func (r *OperatorCertRepository) GetLatestByUserID(userID int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.Where("user_id = ?", userID).Order("id DESC").First(&cert).Error
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// GetPendingByUserID 获取用户待审核的操作证（status=0）
func (r *OperatorCertRepository) GetPendingByUserID(userID int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.Where("user_id = ? AND status = ?", userID, 0).Order("id DESC").First(&cert).Error
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// GetByID 根据ID获取操作证
func (r *OperatorCertRepository) GetByID(id int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.First(&cert, id).Error
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// Delete 删除操作证
func (r *OperatorCertRepository) Delete(id int) error {
	return r.db.Delete(&OperatorCert{}, id).Error
}

// DeleteByUserID 删除用户的操作证
func (r *OperatorCertRepository) DeleteByUserID(userID int) error {
	return r.db.Where("user_id = ?", userID).Delete(&OperatorCert{}).Error
}

// CreatePendingCert 创建待审核操作证
// 如果用户已有审核通过的操作证，则关联old_cert_id
func (r *OperatorCertRepository) CreatePendingCert(userID int, fileName, minioBucket, minioPath string, fileSize int64, fileType string) (*OperatorCert, error) {
	// 查找当前有效的操作证
	activeCert, _ := r.GetActiveByUserID(userID)

	cert := &OperatorCert{
		UserID:      userID,
		FileName:    fileName,
		MinioBucket: minioBucket,
		MinioPath:   minioPath,
		FileSize:    fileSize,
		FileType:    fileType,
		Status:      0, // 待审核
	}

	// 如果有已通过的操作证，记录旧证书ID
	if activeCert != nil {
		cert.OldCertID = &activeCert.ID
	}

	err := r.db.Create(cert).Error
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// ApproveCert 审核通过操作证
func (r *OperatorCertRepository) ApproveCert(certID int, reviewerID int, note string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 获取待审核的操作证
		var newCert OperatorCert
		err := tx.First(&newCert, certID).Error
		if err != nil {
			return err
		}

		// 如果有关联的旧证书，将其状态改为已替换
		if newCert.OldCertID != nil {
			tx.Model(&OperatorCert{}).Where("id = ?", *newCert.OldCertID).Update("status", 2)
		}

		// 将新证书状态改为已通过
		now := time.Now()
		return tx.Model(&newCert).Updates(map[string]interface{}{
			"status":      1,
			"review_note": note,
			"reviewer_id":  reviewerID,
			"review_time": &now,
		}).Error
	})
}

// RejectCert 审核拒绝操作证
func (r *OperatorCertRepository) RejectCert(certID int, reviewerID int, note string) error {
	now := time.Now()
	return r.db.Model(&OperatorCert{}).Where("id = ?", certID).Updates(map[string]interface{}{
		"status":      2,
		"review_note": note,
		"reviewer_id":  reviewerID,
		"review_time": &now,
	}).Error
}

// ListPendingCerts 获取待审核操作证列表（只返回status=0的）
func (r *OperatorCertRepository) ListPendingCerts(limit, page int) ([]*OperatorCert, int64, error) {
	var certs []*OperatorCert
	var total int64
	offset := (page - 1) * limit

	// 获取总数
	r.db.Model(&OperatorCert{}).Where("status = ?", 0).Count(&total)

	// 获取分页数据
	err := r.db.Where("status = ?", 0).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&certs).Error

	return certs, total, err
}

// ListPending 获取待审核���户的操作证列表（用于管理员审批）
func (r *OperatorCertRepository) ListPending(limit, offset int) ([]*UserWithCert, int64, error) {
	return r.ListByCertStatus(0, limit, offset)
}

// ListRejected 获取被拒绝操作证的用户列表
func (r *OperatorCertRepository) ListRejected(limit, offset int) ([]*UserWithCert, int64, error) {
	return r.ListByCertStatus(2, limit, offset)
}

// ListByCertStatus 根据操作证状态获取用户列表
func (r *OperatorCertRepository) ListByCertStatus(certStatus int, limit, offset int) ([]*UserWithCert, int64, error) {
	var users []*User
	var total int64

	// 获取有指定状态操作证的用户ID列表
	var userIDs []int
	r.db.Model(&OperatorCert{}).Where("status = ?", certStatus).Distinct("user_id").Pluck("user_id", &userIDs)

	if len(userIDs) == 0 {
		return []*UserWithCert{}, 0, nil
	}

	// 统计用户数量
	total = int64(len(userIDs))

	// 查询用户及其操作证
	err := r.db.Where("id IN ?", userIDs).
		Order("create_time DESC").
		Limit(limit).
		Offset(offset).
		Find(&users).Error

	if err != nil {
		return nil, 0, err
	}

	// 为每个用户加载指定状态的操作证信息
	result := make([]*UserWithCert, 0, len(users))
	for _, u := range users {
		var cert *OperatorCert
		var certErr error

		if certStatus == 0 {
			cert, certErr = r.GetPendingByUserID(u.ID)
		} else if certStatus == 1 {
			cert, certErr = r.GetActiveByUserID(u.ID)
		} else {
			// 对于被拒绝的，获取最新的
			cert, certErr = r.GetLatestByUserID(u.ID)
			// 确保返回的是被拒绝的
			if cert != nil && cert.Status != 2 {
				cert = nil
			}
		}

		if certErr == nil && cert != nil {
			result = append(result, &UserWithCert{
				User: u,
				Cert: cert,
			})
		}
	}

	return result, total, nil
}

// UserWithCert 用户和操作证组合
type UserWithCert struct {
	User *User
	Cert *OperatorCert
}
