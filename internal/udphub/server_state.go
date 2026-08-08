package udphub

import "draarl/internal/models"

// snapshotConnList 返回当前连接列表快照（只读，禁止修改返回切片）。
func (p *CurrentConnPool) snapshotConnList() []*models.Device {
	if p == nil {
		return nil
	}
	if v := p.devConnList.Load(); v != nil {
		if list, ok := v.([]*models.Device); ok {
			return list
		}
	}
	// 兼容旧代码仍写 DevConnList 字段的路径（过渡期）
	return nil
}

// storeConnList 原子替换连接列表快照。
func (p *CurrentConnPool) storeConnList(list []*models.Device) {
	if p == nil {
		return
	}
	if list == nil {
		list = make([]*models.Device, 0)
	}
	p.devConnList.Store(list)
}
