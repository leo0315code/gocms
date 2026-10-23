package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/example/gocms/internal/model"
)

// SettingRepo 设置仓储
type SettingRepo struct {
	g *gorm.DB
}

// All 全部设置，返回 key -> value
func (r *SettingRepo) All() (map[string]string, error) {
	var list []model.Setting
	if err := r.g.Select("key", "value").Find(&list).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(list))
	for _, s := range list {
		m[s.Key] = s.Value
	}
	return m, nil
}

// ListDefs 全部设置项定义（后台表单）
func (r *SettingRepo) ListDefs() ([]*model.Setting, error) {
	var out []*model.Setting
	err := r.g.Order("`group` ASC, sort ASC, id ASC").Find(&out).Error
	return out, err
}

// Get 取单个值
func (r *SettingRepo) Get(key string) string {
	var s model.Setting
	if err := r.g.Select("value").Where("`key` = ?", key).First(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

// Set 写单个值（存在则更新）
func (r *SettingRepo) Set(key, value string) error {
	return r.SetBatch(map[string]string{key: value})
}

// SetBatch 批量写入（事务 + upsert）
//
// 用 clause.OnConflict 而不是先查后写：一条语句完成，避免并发下的竞态。
// 注意 settings 表的唯一键是 `key`，冲突时只更新 value。
func (r *SettingRepo) SetBatch(kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	list := make([]model.Setting, 0, len(kv))
	for k, v := range kv {
		list = append(list, model.Setting{Key: k, Value: v})
	}
	err := r.g.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&list).Error
	return wrap("保存设置", err)
}

// AddDef 新增设置项定义（存在则更新元信息，但不动已有 value）
func (r *SettingRepo) AddDef(s *model.Setting) error {
	// value 用 COALESCE 技巧：冲突时不覆盖已有值，避免升级时重置用户配置
	err := r.g.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"label": clause.Expr{SQL: "VALUES(`label`)"},
			"group": clause.Expr{SQL: "VALUES(`group`)"},
			"type":  clause.Expr{SQL: "VALUES(`type`)"},
			"sort":  clause.Expr{SQL: "VALUES(`sort`)"},
		}),
	}).Create(s).Error
	return wrap("新增设置项 "+s.Key, err)
}

// ---------- 自定义字段 ----------

// FieldRepo 自定义字段仓储
type FieldRepo struct {
	g *gorm.DB
}

// ListByCategory 某分类下的字段定义
func (r *FieldRepo) ListByCategory(categoryID uint64) ([]*model.Field, error) {
	var out []*model.Field
	err := r.g.Where("category_id = ?", categoryID).
		Order("sort ASC, id ASC").Find(&out).Error
	return out, err
}

// ListAll 全部字段
func (r *FieldRepo) ListAll() ([]*model.Field, error) {
	var out []*model.Field
	err := r.g.Order("category_id ASC, sort ASC, id ASC").Find(&out).Error
	return out, err
}

// ByID 按 ID
func (r *FieldRepo) ByID(id uint64) (*model.Field, error) {
	var f model.Field
	if err := r.g.First(&f, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

// Create 新建
func (r *FieldRepo) Create(f *model.Field) (uint64, error) {
	if err := r.g.Create(f).Error; err != nil {
		return 0, wrap("创建字段", err)
	}
	return f.ID, nil
}

// Update 更新
func (r *FieldRepo) Update(f *model.Field) error {
	// 用 Select 显式列出可改字段：避免 Save 把零值覆盖进去或漏字段
	err := r.g.Model(&model.Field{}).Where("id = ?", f.ID).
		Select("category_id", "name", "label", "type", "options", "placeholder", "required", "sort").
		Updates(f).Error
	return wrap("更新字段", err)
}

// Delete 删除字段及其值（同一事务，避免残留孤儿值）
func (r *FieldRepo) Delete(id uint64) error {
	return r.g.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("field_id = ?", id).Delete(&model.PostFieldValue{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Field{}, id).Error
	})
}

// ValuesOf 取文章字段值（连带字段定义）
//
// 不用 Joins("Field") 预加载：模型里 Field 是运行时字段（gorm:"-"），
// 没有声明外键关联，显式 JOIN + 手动装配更直接也更可控。
func (r *FieldRepo) ValuesOf(postID uint64) ([]*model.PostFieldValue, error) {
	var rows []struct {
		model.PostFieldValue
		FID          uint64 `gorm:"column:f_id"`
		FCategoryID  uint64 `gorm:"column:f_category_id"`
		FName        string `gorm:"column:f_name"`
		FLabel       string `gorm:"column:f_label"`
		FType        string `gorm:"column:f_type"`
		FOptions     string `gorm:"column:f_options"`
		FPlaceholder string `gorm:"column:f_placeholder"`
		FRequired    int    `gorm:"column:f_required"`
		FSort        int    `gorm:"column:f_sort"`
	}
	err := r.g.Table("post_field_values AS v").
		Select(`v.post_id, v.field_id, v.value,
			f.id AS f_id, f.category_id AS f_category_id, f.name AS f_name, f.label AS f_label,
			f.type AS f_type, f.options AS f_options, f.placeholder AS f_placeholder,
			f.required AS f_required, f.sort AS f_sort`).
		Joins("INNER JOIN fields f ON f.id = v.field_id").
		Where("v.post_id = ?", postID).
		Order("f.sort ASC, f.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*model.PostFieldValue, 0, len(rows))
	for _, row := range rows {
		v := row.PostFieldValue
		v.Field = &model.Field{
			ID: row.FID, CategoryID: row.FCategoryID, Name: row.FName, Label: row.FLabel,
			Type: row.FType, Options: row.FOptions, Placeholder: row.FPlaceholder,
			Required: row.FRequired, Sort: row.FSort,
		}
		out = append(out, &v)
	}
	return out, nil
}

// SaveValues 保存文章字段值（覆盖式，空值删除）
func (r *FieldRepo) SaveValues(postID uint64, values map[uint64]string) error {
	if len(values) == 0 {
		return nil
	}
	return r.g.Transaction(func(tx *gorm.DB) error {
		for fieldID, val := range values {
			if val == "" {
				if err := tx.Where("post_id = ? AND field_id = ?", postID, fieldID).
					Delete(&model.PostFieldValue{}).Error; err != nil {
					return err
				}
				continue
			}
			row := model.PostFieldValue{PostID: postID, FieldID: fieldID, Value: val}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "post_id"}, {Name: "field_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"value"}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
