-- 初始化表结构。也可依赖 GORM AutoMigrate，此文件用于生产环境显式管理。

CREATE TABLE IF NOT EXISTS `users` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `username`      VARCHAR(64)     NOT NULL,
  `password_hash` VARCHAR(255)    NOT NULL,
  `created_at`    DATETIME(3)     NULL,
  `updated_at`    DATETIME(3)     NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_users_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `short_urls` (
  `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `user_id`      BIGINT UNSIGNED NOT NULL,
  `code`         VARCHAR(16)     NOT NULL,
  `original_url` VARCHAR(2048)   NOT NULL,
  `visit_count`  BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `created_at`   DATETIME(3)     NULL,
  `updated_at`   DATETIME(3)     NULL,
  PRIMARY KEY (`id`),
  -- 跳转链路依赖该唯一索引，同时兜底短码碰撞。
  UNIQUE KEY `uk_short_urls_code` (`code`),
  KEY `idx_short_urls_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `url_visits` (
  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `url_id`     BIGINT UNSIGNED NOT NULL,
  `ip`         VARCHAR(45)     NULL,
  `user_agent` VARCHAR(512)    NULL,
  `referer`    VARCHAR(512)    NULL,
  `visited_at` DATETIME(3)     NULL,
  PRIMARY KEY (`id`),
  KEY `idx_url_visits_url_id` (`url_id`),
  KEY `idx_url_visits_visited_at` (`visited_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
