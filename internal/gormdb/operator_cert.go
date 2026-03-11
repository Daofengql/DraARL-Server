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

// GetActiveByUserID 获取用户当前有效��操作证（status=1）
func (r *OperatorCertRepository) GetActiveByUserID(userID int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.Where("user_id = ? AND status = ?", userID, 1).First(&cert).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 没有记录时返回 nil 而不是错误
		}
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
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 没有记录时返回 nil 而不是错误
		}
		return nil, err
	}
	return &cert, nil
}

// GetAllCertsByUserID 获取用户的所有操作证（不管状态）
func (r *OperatorCertRepository) GetAllCertsByUserID(userID int) ([]*OperatorCert, error) {
	var certs []*OperatorCert
	err := r.db.Where("user_id = ?", userID).Order("id DESC").Find(&certs).Error
	if err != nil {
		return nil, err
	}
	return certs, nil
}

// GetPendingByUserID 获取用户待审核的操作证（status=0）
func (r *OperatorCertRepository) GetPendingByUserID(userID int) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.Where("user_id = ? AND status = ?", userID, 0).Order("id DESC").First(&cert).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 没有记录时返回 nil 而不是错误
		}
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

// UpdatePendingCert 更新待审核操作证记录（用于重新上传时替换）
func (r *OperatorCertRepository) UpdatePendingCert(certID int, fileName, minioBucket, minioPath string, fileSize int64, fileType string) (*OperatorCert, error) {
	var cert OperatorCert
	err := r.db.First(&cert, certID).Error
	if err != nil {
		return nil, err
	}

	// 只允许更新待审核状态的记录
	if cert.Status != 0 {
		return nil, gorm.ErrInvalidData
	}

	cert.FileName = fileName
	cert.MinioBucket = minioBucket
	cert.MinioPath = minioPath
	cert.FileSize = fileSize
	cert.FileType = fileType
	// 清除之前的审核信息
	cert.ReviewNote = ""
	cert.ReviewTime = nil
	cert.ReviewerID = nil

	err = r.db.Save(&cert).Error
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// ApproveCert 审核通过操作证
func (r *OperatorCertRepository) ApproveCert(certID int, reviewerID int, note string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 1. 获取当前待审核的操作证
		var newCert OperatorCert
		err := tx.First(&newCert, certID).Error
		if err != nil {
			return err
		}

		// 2. 如果该次上传是为了替换旧证书，则将旧证书的状态置为已替换(3)
		if newCert.OldCertID != nil {
			if err := tx.Model(&OperatorCert{}).Where("id = ?", *newCert.OldCertID).Update("status", 3).Error; err != nil {
				return err
			}
		}

		// 3. 将新的操作证状态改为已通过(1)，并记录审核信息
		now := time.Now()
		err = tx.Model(&newCert).Updates(map[string]interface{}{
			"status":      1,
			"review_note": note,
			"reviewer_id": reviewerID,
			"review_time": &now,
		}).Error
		if err != nil {
			return err
		}

		// 4. 同步更新关联用户的整体审批状态为已通过(1)
		return tx.Model(&User{}).Where("id = ?", newCert.UserID).Updates(map[string]interface{}{
			"approval_status": 1,
			"reviewer_id":     reviewerID,
			"review_note":     note,
			"review_time":     &now,
		}).Error
	})
}

// RejectCert 审核拒绝操��证
func (r *OperatorCertRepository) RejectCert(certID int, reviewerID int, note string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 获取待审核的操作证
		var cert OperatorCert
		err := tx.First(&cert, certID).Error
		if err != nil {
			return err
		}

		// 拒绝时保留 old_cert_id，以便前端显示该证书是首次上传还是更新操作
		now := time.Now()
		return tx.Model(&cert).Updates(map[string]interface{}{
			"status":      2,
			"review_note": note,
			"reviewer_id": reviewerID,
			"review_time": &now,
		}).Error
	})
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

// ListByCertStatus 根据操作证状态获取用户列表（保留旧方法兼容）
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

	// 查询用户
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
			cert, certErr = r.GetLatestByUserID(u.ID)
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

// UserWithCerts 用户和多个操作证的组合
type UserWithCerts struct {
	User  *User
	Certs []*OperatorCert
}

// ListPendingWithCerts 获取待审核用户的操作证列表（包含所有证书）
func (r *OperatorCertRepository) ListPendingWithCerts(limit, offset int) ([]*UserWithCerts, int64, error) {
	return r.ListByCertStatusWithAllCerts(0, limit, offset)
}

// ListRejectedWithCerts 获取被拒绝操作证的用户列表（包含所有证书）
func (r *OperatorCertRepository) ListRejectedWithCerts(limit, offset int) ([]*UserWithCerts, int64, error) {
	return r.ListByCertStatusWithAllCerts(2, limit, offset)
}

// ListByCertStatusWithAllCerts 根据操作证状态获取用户列表，并返回所有证书
func (r *OperatorCertRepository) ListByCertStatusWithAllCerts(certStatus int, limit, offset int) ([]*UserWithCerts, int64, error) {
	var users []*User
	var total int64

	// 获取有指定状态操作证的用户ID列表
	var userIDs []int
	r.db.Model(&OperatorCert{}).Where("status = ?", certStatus).Distinct("user_id").Pluck("user_id", &userIDs)

	if len(userIDs) == 0 {
		return []*UserWithCerts{}, 0, nil
	}

	// 统计用户数量
	total = int64(len(userIDs))

	// 查询用户
	err := r.db.Where("id IN ?", userIDs).
		Order("create_time DESC").
		Limit(limit).
		Offset(offset).
		Find(&users).Error

	if err != nil {
		return nil, 0, err
	}

	// 为每个用户加载所有操作证信息
	result := make([]*UserWithCerts, 0, len(users))
	for _, u := range users {
		// 获取该用户的所有操作证（不管状态）
		var certs []*OperatorCert
		certErr := r.db.Where("user_id = ?", u.ID).Order("id DESC").Find(&certs).Error

		if certErr == nil {
			result = append(result, &UserWithCerts{
				User:  u,
				Certs: certs,
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

// CertificateApproval 操作证审批项
type CertificateApproval struct {
	ID          int
	UserID      int
	UserName    string
	NickName    string
	CallSign    string
	FileName    string
	MinioPath   string
	FileSize    int64
	FileType    string
	UploadTime  time.Time
	Status      int
	ReviewNote  string
	ReviewTime  *time.Time
	ReviewerID  *int
	OldCertID   *int
	IsUpdate    bool // true=更新(非首次), false=首次
	IsReplaced  bool // true=被新证替换(但之前是通过), false=未替换或真正被拒绝
}

// ListCertificateApprovals 获取操作证审批列表（按上传记录列出）
// status: 0=待审核, 1=已通过, 2=已拒绝, -1=全部
func (r *OperatorCertRepository) ListCertificateApprovals(status int, limit, offset int) ([]*CertificateApproval, int64, error) {
	var certs []*OperatorCert
	var total int64

	query := r.db.Model(&OperatorCert{})

	if status >= 0 {
		switch status {
		case 0:
			// 待审核：status = 0
			query = query.Where("status = ?", 0)
		case 1:
			// 已通过：status = 1（当前有效） OR status = 3（已被替换，但之前是通过的）
			query = query.Where("status IN (1, 3)")
		case 2:
			// 已拒绝：status = 2（真正被拒绝，从未通过）
			query = query.Where("status = ?", 2)
		}
	}

	// 获取总数
	query.Count(&total)

	// 获取分页数据
	err := query.Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&certs).Error
	if err != nil {
		return nil, 0, err
	}

	// 获取所有相关用户ID
	userIDs := make([]int, 0, len(certs))
	for _, cert := range certs {
		userIDs = append(userIDs, cert.UserID)
	}

	// 查询用户信息
	var users []*User
	if len(userIDs) > 0 {
		r.db.Where("id IN ?", userIDs).Find(&users)
	}
	userMap := make(map[int]*User)
	for _, u := range users {
		userMap[u.ID] = u
	}

	// 构建结果
	result := make([]*CertificateApproval, 0, len(certs))
	for _, cert := range certs {
		u := userMap[cert.UserID]
		userName := ""
		nickName := ""
		callSign := ""
		if u != nil {
			userName = u.Name
			nickName = u.NickName
			callSign = u.CallSign
		}

		isUpdate := cert.OldCertID != nil
		isReplaced := cert.Status == 3

		result = append(result, &CertificateApproval{
			ID:          cert.ID,
			UserID:      cert.UserID,
			UserName:    userName,
			NickName:    nickName,
			CallSign:    callSign,
			FileName:    cert.FileName,
			MinioPath:   cert.MinioPath,
			FileSize:    cert.FileSize,
			FileType:    cert.FileType,
			UploadTime:  cert.UploadTime,
			Status:      cert.Status,
			ReviewNote:  cert.ReviewNote,
			ReviewTime:  cert.ReviewTime,
			ReviewerID:  cert.ReviewerID,
			OldCertID:   cert.OldCertID,
			IsUpdate:    isUpdate,
			IsReplaced:  isReplaced,
		})
	}

	return result, total, nil
}
