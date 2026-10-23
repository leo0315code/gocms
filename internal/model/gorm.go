package model

import "time"

// 本文件为 GORM 映射标签说明。
//
// 约定：
//   - 保留原有 json 标签与「运行时字段」（Children/PostCount/Parent 等）不参与映射，统一 gorm:"-"
//   - 表名一律显式声明 TableName()，不依赖 GORM 的复数推断（media 这类词推断不可靠）
//   - 数据库迁移仍由 migrations/*.sql 负责，不使用 AutoMigrate，避免 ORM 擅自改线上表结构
//
// 字段类型对应：
//   - 表里的 TINYINT 布尔语义字段（is_nav/is_show/is_top/is_recommend/required/status）沿用 int
//   - DATETIME NULL 用 *time.Time
//   - 无时间戳的表（settings/fields/menu_items/menus/post_field_values）不声明 CreatedAt/UpdatedAt

// User 后台用户
type User struct {
	ID          uint64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Username    string     `json:"username" gorm:"column:username"`
	Password    string     `json:"-" gorm:"column:password"`
	Email       string     `json:"email" gorm:"column:email"`
	Nickname    string     `json:"nickname" gorm:"column:nickname"`
	Avatar      string     `json:"avatar" gorm:"column:avatar"`
	Role        string     `json:"role" gorm:"column:role"`
	Status      int        `json:"status" gorm:"column:status"`
	LastLoginAt *time.Time `json:"last_login_at" gorm:"column:last_login_at"`
	LastLoginIP string     `json:"last_login_ip" gorm:"column:last_login_ip"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

// TableName 表名
func (User) TableName() string { return "users" }

// Session 会话
type Session struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Token     string    `gorm:"column:token"`
	UserID    uint64    `gorm:"column:user_id"`
	CSRFToken string    `gorm:"column:csrf_token"`
	IP        string    `gorm:"column:ip"`
	UserAgent string    `gorm:"column:user_agent"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 表名
func (Session) TableName() string { return "sessions" }

// Category 分类
type Category struct {
	ID             uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ParentID       uint64    `json:"parent_id" gorm:"column:parent_id"`
	Name           string    `json:"name" gorm:"column:name"`
	Slug           string    `json:"slug" gorm:"column:slug"`
	Description    string    `json:"description" gorm:"column:description"`
	Cover          string    `json:"cover" gorm:"column:cover"`
	Icon           string    `json:"icon" gorm:"column:icon"`
	Type           string    `json:"type" gorm:"column:type"`
	Template       string    `json:"template" gorm:"column:template"`
	Sort           int       `json:"sort" gorm:"column:sort"`
	IsNav          int       `json:"is_nav" gorm:"column:is_nav"`
	SEOTitle       string    `json:"seo_title" gorm:"column:seo_title"`
	SEOKeywords    string    `json:"seo_keywords" gorm:"column:seo_keywords"`
	SEODescription string    `json:"seo_description" gorm:"column:seo_description"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"column:updated_at"`

	// 运行时字段
	Children  []*Category `json:"children,omitempty" gorm:"-"`
	PostCount int         `json:"post_count" gorm:"-"`
	Parent    *Category   `json:"-" gorm:"-"`
}

// TableName 表名
func (Category) TableName() string { return "categories" }

// Post 文章
type Post struct {
	ID             uint64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	CategoryID     uint64     `json:"category_id" gorm:"column:category_id"`
	UserID         uint64     `json:"user_id" gorm:"column:user_id"`
	Title          string     `json:"title" gorm:"column:title"`
	Slug           string     `json:"slug" gorm:"column:slug"`
	Summary        string     `json:"summary" gorm:"column:summary"`
	Content        string     `json:"content" gorm:"column:content"`
	ContentFormat  string     `json:"content_format" gorm:"column:content_format"`
	Cover          string     `json:"cover" gorm:"column:cover"`
	Status         string     `json:"status" gorm:"column:status"`
	IsTop          int        `json:"is_top" gorm:"column:is_top"`
	IsRecommend    int        `json:"is_recommend" gorm:"column:is_recommend"`
	Views          int        `json:"views" gorm:"column:views"`
	Sort           int        `json:"sort" gorm:"column:sort"`
	Template       string     `json:"template" gorm:"column:template"`
	SEOTitle       string     `json:"seo_title" gorm:"column:seo_title"`
	SEOKeywords    string     `json:"seo_keywords" gorm:"column:seo_keywords"`
	SEODescription string     `json:"seo_description" gorm:"column:seo_description"`
	PublishedAt    *time.Time `json:"published_at" gorm:"column:published_at"`
	CreatedAt      time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time  `json:"updated_at" gorm:"column:updated_at"`

	// 关联（不是列，查询时按需填充）
	Category *Category         `json:"category,omitempty" gorm:"-"`
	Author   *User             `json:"author,omitempty" gorm:"-"`
	Tags     []*Tag            `json:"tags,omitempty" gorm:"-"`
	Fields   []*PostFieldValue `json:"fields,omitempty" gorm:"-"`
}

// TableName 表名
func (Post) TableName() string { return "posts" }

// Page 单页
type Page struct {
	ID             uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Title          string    `json:"title" gorm:"column:title"`
	Slug           string    `json:"slug" gorm:"column:slug"`
	Content        string    `json:"content" gorm:"column:content"`
	ContentFormat  string    `json:"content_format" gorm:"column:content_format"`
	Cover          string    `json:"cover" gorm:"column:cover"`
	Template       string    `json:"template" gorm:"column:template"`
	Status         string    `json:"status" gorm:"column:status"`
	Sort           int       `json:"sort" gorm:"column:sort"`
	IsNav          int       `json:"is_nav" gorm:"column:is_nav"`
	SEOTitle       string    `json:"seo_title" gorm:"column:seo_title"`
	SEOKeywords    string    `json:"seo_keywords" gorm:"column:seo_keywords"`
	SEODescription string    `json:"seo_description" gorm:"column:seo_description"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"column:updated_at"`
}

// TableName 表名
func (Page) TableName() string { return "pages" }

// Tag 标签
type Tag struct {
	ID        uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Name      string    `json:"name" gorm:"column:name"`
	Slug      string    `json:"slug" gorm:"column:slug"`
	Count     int       `json:"count" gorm:"column:count"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (Tag) TableName() string { return "tags" }

// Media 媒体
type Media struct {
	ID        uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint64    `json:"user_id" gorm:"column:user_id"`
	Name      string    `json:"name" gorm:"column:name"`
	Path      string    `json:"path" gorm:"column:path"`
	URL       string    `json:"url" gorm:"column:url"`
	Mime      string    `json:"mime" gorm:"column:mime"`
	Ext       string    `json:"ext" gorm:"column:ext"`
	Size      int64     `json:"size" gorm:"column:size"`
	Width     int       `json:"width" gorm:"column:width"`
	Height    int       `json:"height" gorm:"column:height"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (Media) TableName() string { return "media" }

// Menu 菜单
type Menu struct {
	ID    uint64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Name  string      `json:"name" gorm:"column:name"`
	Key   string      `json:"key" gorm:"column:key"`
	Items []*MenuItem `json:"items,omitempty" gorm:"-"`
}

// TableName 表名
func (Menu) TableName() string { return "menus" }

// MenuItem 菜单项
type MenuItem struct {
	ID       uint64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	MenuID   uint64      `json:"menu_id" gorm:"column:menu_id"`
	ParentID uint64      `json:"parent_id" gorm:"column:parent_id"`
	Title    string      `json:"title" gorm:"column:title"`
	LinkType string      `json:"link_type" gorm:"column:link_type"`
	LinkID   uint64      `json:"link_id" gorm:"column:link_id"`
	URL      string      `json:"url" gorm:"column:url"`
	Target   string      `json:"target" gorm:"column:target"`
	Icon     string      `json:"icon" gorm:"column:icon"`
	Sort     int         `json:"sort" gorm:"column:sort"`
	IsShow   int         `json:"is_show" gorm:"column:is_show"`
	Children []*MenuItem `json:"children,omitempty" gorm:"-"`
}

// TableName 表名
func (MenuItem) TableName() string { return "menu_items" }

// Banner 轮播
type Banner struct {
	ID         uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Title      string    `json:"title" gorm:"column:title"`
	Subtitle   string    `json:"subtitle" gorm:"column:subtitle"`
	Image      string    `json:"image" gorm:"column:image"`
	URL        string    `json:"url" gorm:"column:url"`
	ButtonText string    `json:"button_text" gorm:"column:button_text"`
	Position   string    `json:"position" gorm:"column:position"`
	Sort       int       `json:"sort" gorm:"column:sort"`
	IsShow     int       `json:"is_show" gorm:"column:is_show"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (Banner) TableName() string { return "banners" }

// Link 友情链接
type Link struct {
	ID          uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"column:name"`
	URL         string    `json:"url" gorm:"column:url"`
	Logo        string    `json:"logo" gorm:"column:logo"`
	Description string    `json:"description" gorm:"column:description"`
	Sort        int       `json:"sort" gorm:"column:sort"`
	IsShow      int       `json:"is_show" gorm:"column:is_show"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (Link) TableName() string { return "links" }

// Message 留言
type Message struct {
	ID        uint64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Name      string     `json:"name" gorm:"column:name"`
	Phone     string     `json:"phone" gorm:"column:phone"`
	Email     string     `json:"email" gorm:"column:email"`
	Company   string     `json:"company" gorm:"column:company"`
	Content   string     `json:"content" gorm:"column:content"`
	SourceURL string     `json:"source_url" gorm:"column:source_url"`
	IP        string     `json:"ip" gorm:"column:ip"`
	UserAgent string     `json:"user_agent" gorm:"column:user_agent"`
	Status    string     `json:"status" gorm:"column:status"`
	Reply     string     `json:"reply" gorm:"column:reply"`
	RepliedAt *time.Time `json:"replied_at" gorm:"column:replied_at"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (Message) TableName() string { return "messages" }

// Setting 设置
type Setting struct {
	ID    uint64 `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	Key   string `json:"key" gorm:"column:key"`
	Value string `json:"value" gorm:"column:value"`
	Group string `json:"group" gorm:"column:group"`
	Label string `json:"label" gorm:"column:label"`
	Type  string `json:"type" gorm:"column:type"`
	Sort  int    `json:"sort" gorm:"column:sort"`
}

// TableName 表名
func (Setting) TableName() string { return "settings" }

// Field 自定义字段定义
type Field struct {
	ID          uint64 `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	CategoryID  uint64 `json:"category_id" gorm:"column:category_id"`
	Name        string `json:"name" gorm:"column:name"`
	Label       string `json:"label" gorm:"column:label"`
	Type        string `json:"type" gorm:"column:type"`
	Options     string `json:"options" gorm:"column:options"`
	Placeholder string `json:"placeholder" gorm:"column:placeholder"`
	Required    int    `json:"required" gorm:"column:required"`
	Sort        int    `json:"sort" gorm:"column:sort"`
}

// TableName 表名
func (Field) TableName() string { return "fields" }

// PostFieldValue 自定义字段值
type PostFieldValue struct {
	PostID  uint64 `json:"post_id" gorm:"column:post_id;primaryKey"`
	FieldID uint64 `json:"field_id" gorm:"column:field_id;primaryKey"`
	Value   string `json:"value" gorm:"column:value"`
	Field   *Field `json:"field,omitempty" gorm:"-"`
}

// TableName 表名
func (PostFieldValue) TableName() string { return "post_field_values" }

// PostTag 文章-标签关联（中间表）
type PostTag struct {
	PostID uint64 `gorm:"column:post_id;primaryKey"`
	TagID  uint64 `gorm:"column:tag_id;primaryKey"`
}

// TableName 表名
func (PostTag) TableName() string { return "post_tags" }

// AuditLog 操作审计日志
type AuditLog struct {
	ID        uint64    `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint64    `json:"user_id" gorm:"column:user_id"`
	Username  string    `json:"username" gorm:"column:username"`
	Action    string    `json:"action" gorm:"column:action"`
	Target    string    `json:"target" gorm:"column:target"`
	Detail    string    `json:"detail" gorm:"column:detail"`
	IP        string    `json:"ip" gorm:"column:ip"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

// TableName 表名
func (AuditLog) TableName() string { return "audit_logs" }
