package model

// 本文件只保留模型方法。
// 结构体定义（含 GORM 映射标签）统一在 gorm.go，避免两处定义冲突。

// DisplayName 昵称优先，其次用户名
func (u *User) DisplayName() string {
	if u == nil {
		return ""
	}
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}

// IsAdmin 是否超级管理员
func (u *User) IsAdmin() bool {
	return u != nil && u.Role == "admin"
}

// CanEdit 是否可写内容
func (u *User) CanEdit() bool {
	if u == nil {
		return false
	}
	return u.Role == "admin" || u.Role == "editor"
}

// IsImage 是否图片
func (m *Media) IsImage() bool {
	return len(m.Mime) > 5 && m.Mime[:5] == "image"
}

// SizeText 人类可读的大小
func (m *Media) SizeText() string {
	const unit = 1024
	if m.Size < unit {
		return itoa(m.Size) + " B"
	}
	div, exp := int64(unit), 0
	for n := m.Size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return trimZero(float64(m.Size)/float64(div)) + " " + []string{"KB", "MB", "GB"}[exp]
}

// OptionsList 把 options 按行拆分供下拉渲染
func (f *Field) OptionsList() []string {
	var out []string
	cur := ""
	for _, r := range f.Options {
		if r == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		if r != '\r' {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
