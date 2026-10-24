package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/leo0315code/gocms/internal/model"
)

// MenuRepo 菜单仓储
type MenuRepo struct {
	g *gorm.DB
}

// List 全部菜单（不含菜单项）
func (r *MenuRepo) List() ([]*model.Menu, error) {
	var out []*model.Menu
	if err := r.g.Order("id ASC").Find(&out).Error; err != nil {
		return nil, wrap("查询菜单列表", err)
	}
	return out, nil
}

// ByKey 按 key 取菜单树
func (r *MenuRepo) ByKey(key string) (*model.Menu, error) {
	var m model.Menu
	if err := r.g.Where("`key` = ?", key).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("按标识查询菜单", err)
	}
	items, err := r.Items(m.ID)
	if err != nil {
		return nil, err
	}
	m.Items = items
	return &m, nil
}

// ByID 按 ID
func (r *MenuRepo) ByID(id uint64) (*model.Menu, error) {
	var m model.Menu
	if err := r.g.First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询菜单", err)
	}
	items, err := r.Items(m.ID)
	if err != nil {
		return nil, err
	}
	m.Items = items
	return &m, nil
}

// Items 取菜单项树
func (r *MenuRepo) Items(menuID uint64) ([]*model.MenuItem, error) {
	var flat []*model.MenuItem
	err := r.g.Where("menu_id = ?", menuID).
		Order("sort ASC, id ASC").Find(&flat).Error
	if err != nil {
		return nil, wrap("查询菜单项", err)
	}
	return buildMenuTree(flat), nil
}

func buildMenuTree(flat []*model.MenuItem) []*model.MenuItem {
	byID := make(map[uint64]*model.MenuItem, len(flat))
	for _, it := range flat {
		it.Children = nil
		byID[it.ID] = it
	}
	var roots []*model.MenuItem
	for _, it := range flat {
		if it.ParentID == 0 {
			roots = append(roots, it)
			continue
		}
		if p, ok := byID[it.ParentID]; ok {
			p.Children = append(p.Children, it)
		} else {
			roots = append(roots, it)
		}
	}
	return roots
}

// FlatItems 取菜单项平铺列表（后台编辑用）
func (r *MenuRepo) FlatItems(menuID uint64) ([]*model.MenuItem, error) {
	var out []*model.MenuItem
	err := r.g.Where("menu_id = ?", menuID).
		Order("parent_id ASC, sort ASC, id ASC").Find(&out).Error
	if err != nil {
		return nil, wrap("查询菜单项列表", err)
	}
	return out, nil
}

// CreateMenu 新建菜单
func (r *MenuRepo) CreateMenu(m *model.Menu) (uint64, error) {
	if err := r.g.Create(m).Error; err != nil {
		return 0, wrap("创建菜单", err)
	}
	return m.ID, nil
}

// DeleteMenu 删除菜单及其菜单项
func (r *MenuRepo) DeleteMenu(id uint64) error {
	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("menu_id = ?", id).Delete(&model.MenuItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Menu{}).Error
	})
	return wrap("删除菜单", err)
}

// CreateItem 新建菜单项
func (r *MenuRepo) CreateItem(it *model.MenuItem) (uint64, error) {
	if err := r.g.Create(it).Error; err != nil {
		return 0, wrap("创建菜单项", err)
	}
	return it.ID, nil
}

// UpdateItem 更新菜单项
func (r *MenuRepo) UpdateItem(it *model.MenuItem) error {
	err := r.g.Model(&model.MenuItem{}).Where("id = ?", it.ID).
		Updates(map[string]any{
			"parent_id": it.ParentID,
			"title":     it.Title,
			"link_type": it.LinkType,
			"link_id":   it.LinkID,
			"url":       it.URL,
			"target":    it.Target,
			"icon":      it.Icon,
			"sort":      it.Sort,
			"is_show":   it.IsShow,
		}).Error
	return wrap("更新菜单项", err)
}

// ItemByID 按 ID 取菜单项
func (r *MenuRepo) ItemByID(id uint64) (*model.MenuItem, error) {
	var it model.MenuItem
	if err := r.g.First(&it, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, wrap("查询菜单项", err)
	}
	return &it, nil
}

// DeleteItem 删除菜单项（子项上提）
func (r *MenuRepo) DeleteItem(id uint64) error {
	err := r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.MenuItem{}).Where("parent_id = ?", id).
			Update("parent_id", 0).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.MenuItem{}).Error
	})
	return wrap("删除菜单项", err)
}
