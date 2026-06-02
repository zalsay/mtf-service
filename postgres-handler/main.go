package main

import (
    "log"
    "time"

    "github.com/gin-contrib/cors"
    "github.com/gin-gonic/gin"
)

func main() {
    handler, err := NewDatabaseHandler()
    if err != nil {
        log.Fatal("Failed to create database handler:", err)
    }
    defer handler.Close()

    r := gin.Default()
    r.Use(cors.New(cors.Config{
        AllowOriginFunc:  func(origin string) bool { return true },
        AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "Content-Length"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: false,
        MaxAge:           12 * time.Hour,
    }))

    apiToken := getEnv("API_TOKEN", "fintrack-dev-token")
    RegisterRoutes(r, handler, apiToken)

    port := getEnv("PORT", "58004")
    log.Printf("Server starting on port %s", port)
    if err := r.Run(":" + port); err != nil {
        log.Fatal("Server failed to start:", err)
    }
}

