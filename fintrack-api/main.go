package main

import (
	"log"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/routes"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// 连接数据库
	db, err := database.NewConnection(cfg)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// 初始化数据库schema (已禁用 - 改为手动管理)
	// WARNING: InitializeSchema() 包含 DROP TABLE 语句，会清空现有数据
	// 如需初始化数据库，请手动执行 database/schema.sql 文件
	// if err := db.InitializeSchema(); err != nil {
	// 	log.Fatal("Failed to initialize database schema:", err)
	// }

	// 设置路由
	router := routes.SetupRouter(cfg, db)

	// 启动服务器
	log.Printf("Starting server on port %s", cfg.Server.Port)
	if err := router.Run(":" + cfg.Server.Port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
