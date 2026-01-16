package data

import (
	"log"
	"os"
	"path/filepath"

	"stock-ai/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"
)

var DB *gorm.DB

// InitDB 初始化数据库
func InitDB() error {
	// 获取用户数据目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	dataDir := filepath.Join(homeDir, ".stock-ai")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	dbPath := filepath.Join(dataDir, "stock-ai.db")
	log.Printf("数据库路径: %s", dbPath)

	// 打开数据库
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return err
	}

	// 启用WAL模式
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA synchronous=NORMAL")

	// 自动迁移
	err = db.AutoMigrate(
		&models.Stock{},
		&models.Fund{},
		&models.Config{},
		&models.Position{},
		&models.AIMessage{},
		&models.AIAnalysisResult{},
		// 新增：全球市场相关模型
		&models.Futures{},
		&models.USStock{},
		&models.HKStock{},
		// 股票提醒
		&models.StockAlert{},
	)
	if err != nil {
		return err
	}

	// 初始化默认配置
	var config models.Config
	if db.First(&config).Error == gorm.ErrRecordNotFound {
		config = models.Config{
			RefreshInterval: 5,
			AiEnabled:       false,
			AiModel:         "deepseek",
		}
		db.Create(&config)
	}

	DB = db
	return nil
}

// GetDB 获取数据库实例
func GetDB() *gorm.DB {
	return DB
}
