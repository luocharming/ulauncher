package model

import (
	"time"
)

// ModelAsset 模型资产数据模型
type ModelAsset struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	UUID          string    `gorm:"uniqueIndex;size:36;not null" json:"uuid"`                    // 模型唯一标识
	Name          string    `gorm:"size:255;not null" json:"name"`                               // 模型名称
	Type          string    `gorm:"size:50;not null;index" json:"type"`                          // 资产类型: Blueprint, StaticMesh, Material, Texture
	Path          string    `gorm:"size:500" json:"path"`                                        // UE资产路径
	Version       string    `gorm:"size:20;not null;index" json:"version"`                       // 版本号 (semver格式)
	Category      string    `gorm:"size:100;index" json:"category"`                              // 分类
	Tags          string    `gorm:"type:text" json:"tags"`                                       // 标签 (JSON数组字符串)
	Description   string    `gorm:"type:text" json:"description"`                                // 描述
	Author        string    `gorm:"size:100" json:"author"`                                      // 作者
	SizeBytes     int64     `json:"sizeBytes"`                                                   // 文件大小(字节)
	VertexCount   int       `json:"vertexCount"`                                                 // 顶点数
	TriangleCount int       `json:"triangleCount"`                                               // 三角形数
	MaterialCount int       `json:"materialCount"`                                               // 材质数
	PakFile       string    `gorm:"size:255" json:"pakFile"`                                     // Pak文件名
	PakMountPoint string    `gorm:"size:255" json:"pakMountPoint"`                               // Pak挂载点
	CreatedTime   time.Time `gorm:"autoCreateTime" json:"createdTime"`                           // 创建时间
	ModifiedTime  time.Time `gorm:"autoUpdateTime" json:"modifiedTime"`                          // 修改时间
	Thumbnail64   string    `gorm:"size:500" json:"thumbnail64"`                                 // 64x64缩略图路径
	Thumbnail256  string    `gorm:"size:500" json:"thumbnail256"`                                // 256x256缩略图路径
}

// TableName 指定表名
func (ModelAsset) TableName() string {
	return "model_assets"
}

// ModelAssetMetadata 模型元数据结构 (对应metadata.json)
type ModelAssetMetadata struct {
	AssetInfo struct {
		Name         string    `json:"name"`
		Type         string    `json:"type"`
		Path         string    `json:"path"`
		UUID         string    `json:"uuid"`
		Version      string    `json:"version"`
		CreatedTime  time.Time `json:"created_time"`
		ModifiedTime time.Time `json:"modified_time"`
		Description  string    `json:"description"`
	} `json:"asset_info"`
	FileInfo struct {
		SizeBytes     int64  `json:"size_bytes"`
		PakFile       string `json:"pak_file"`
		PakMountPoint string `json:"pak_mount_point"`
	} `json:"file_info"`
	Geometry struct {
		BoundingBox struct {
			Min struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
				Z float64 `json:"z"`
			} `json:"min"`
			Max struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
				Z float64 `json:"z"`
			} `json:"max"`
		} `json:"bounding_box"`
		VertexCount   int `json:"vertex_count"`
		TriangleCount int `json:"triangle_count"`
		MaterialCount int `json:"material_count"`
	} `json:"geometry"`
	Dependencies map[string]interface{} `json:"dependencies"`
	Metadata     struct {
		Tags     []string `json:"tags"`
		Category string   `json:"category"`
		Author   string   `json:"author"`
	} `json:"metadata"`
	PakManifest struct {
		ChunkID   int  `json:"chunk_id"`
		Priority  int  `json:"priority"`
		Preload   bool `json:"preload"`
		LoadOrder int  `json:"load_order"`
	} `json:"pak_manifest"`
}
