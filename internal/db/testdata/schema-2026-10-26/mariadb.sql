CREATE TABLE `migrations` (
  `id` varchar(255) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `nets` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `talkgroup_id` bigint(20) unsigned NOT NULL,
  `started_by_user_id` bigint(20) unsigned DEFAULT NULL,
  `scheduled_net_id` bigint(20) unsigned DEFAULT NULL,
  `start_time` datetime(3) DEFAULT NULL,
  `end_time` datetime(3) DEFAULT NULL,
  `duration_minutes` bigint(20) unsigned DEFAULT NULL,
  `description` longtext DEFAULT NULL,
  `active` tinyint(1) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  `showcase` tinyint(1) DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_nets_talkgroup_id` (`talkgroup_id`),
  KEY `idx_nets_active` (`active`),
  KEY `idx_nets_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `password_resets` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `user_id` bigint(20) unsigned NOT NULL,
  `token_hash` varbinary(32) NOT NULL,
  `expires_at` datetime(3) NOT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_password_resets_token_hash` (`token_hash`),
  KEY `idx_password_resets_user_id` (`user_id`),
  KEY `idx_password_resets_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `plugin_hosts` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `plugin_id` bigint(20) unsigned NOT NULL,
  `address` varchar(255) NOT NULL,
  `reason` varchar(200) NOT NULL DEFAULT '',
  `allowed` tinyint(1) NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_plugin_hosts_plugin_id` (`plugin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `plugin_modules` (
  `plugin_id` bigint(20) unsigned NOT NULL,
  `hash` varbinary(32) NOT NULL,
  `wasm` longblob NOT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`plugin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `plugin_ranges` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `plugin_id` bigint(20) unsigned NOT NULL,
  `first_id` bigint(20) unsigned NOT NULL,
  `last_id` bigint(20) unsigned NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_plugin_ranges_plugin_id` (`plugin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `plugins` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(64) NOT NULL,
  `token_hash` varbinary(32) NOT NULL,
  `token_created_at` datetime(3) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `last_connected_at` datetime(3) DEFAULT NULL,
  `last_seen_at` datetime(3) DEFAULT NULL,
  `last_address` varchar(64) DEFAULT NULL,
  `session` varchar(32) NOT NULL DEFAULT '',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `kind` varchar(16) NOT NULL DEFAULT 'external',
  `builtin` varchar(32) NOT NULL DEFAULT '',
  `version` varchar(32) NOT NULL DEFAULT '',
  `author` varchar(64) NOT NULL DEFAULT '',
  `description` varchar(500) NOT NULL DEFAULT '',
  `module_hash` varbinary(32) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_plugins_name` (`name`),
  UNIQUE KEY `idx_plugins_token_hash` (`token_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `repeaters` (
  `connected` datetime(3) DEFAULT NULL,
  `last_ping` datetime(3) DEFAULT NULL,
  `password` longtext DEFAULT NULL,
  `type` varchar(191) DEFAULT 'mmdvm',
  `dialect` longtext DEFAULT NULL,
  `ts1_dynamic_talkgroup_id` bigint(20) unsigned DEFAULT NULL,
  `ts2_dynamic_talkgroup_id` bigint(20) unsigned DEFAULT NULL,
  `owner_id` bigint(20) unsigned DEFAULT NULL,
  `simplex_repeater` tinyint(1) DEFAULT NULL,
  `trusted` tinyint(1) DEFAULT 0,
  `city` longtext DEFAULT NULL,
  `state` longtext DEFAULT NULL,
  `country` longtext DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  `callsign` longtext DEFAULT NULL,
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `rx_frequency` bigint(20) unsigned DEFAULT NULL,
  `tx_frequency` bigint(20) unsigned DEFAULT NULL,
  `tx_power` tinyint(3) unsigned DEFAULT NULL,
  `color_code` tinyint(3) unsigned DEFAULT NULL,
  `latitude` double DEFAULT NULL,
  `longitude` double DEFAULT NULL,
  `height` smallint(5) unsigned DEFAULT NULL,
  `location` longtext DEFAULT NULL,
  `description` longtext DEFAULT NULL,
  `url` longtext DEFAULT NULL,
  `software_id` longtext DEFAULT NULL,
  `package_id` longtext DEFAULT NULL,
  `slots` bigint(20) unsigned DEFAULT NULL,
  `ts1_codec` varchar(8) NOT NULL DEFAULT 'auto',
  `ts2_codec` varchar(8) NOT NULL DEFAULT 'auto',
  `ts1_heard_codec` varchar(8) NOT NULL DEFAULT '',
  `ts2_heard_codec` varchar(8) NOT NULL DEFAULT '',
  `ts1_heard_codec2_mode` bigint(20) NOT NULL DEFAULT 0,
  `ts2_heard_codec2_mode` bigint(20) NOT NULL DEFAULT 0,
  `ts1_heard_at` datetime(3) DEFAULT NULL,
  `ts2_heard_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_repeaters_deleted_at` (`deleted_at`),
  KEY `fk_users_repeaters` (`owner_id`),
  KEY `fk_repeaters_ts1_dynamic_talkgroup` (`ts1_dynamic_talkgroup_id`),
  KEY `fk_repeaters_ts2_dynamic_talkgroup` (`ts2_dynamic_talkgroup_id`),
  CONSTRAINT `fk_repeaters_ts1_dynamic_talkgroup` FOREIGN KEY (`ts1_dynamic_talkgroup_id`) REFERENCES `talkgroups` (`id`),
  CONSTRAINT `fk_repeaters_ts2_dynamic_talkgroup` FOREIGN KEY (`ts2_dynamic_talkgroup_id`) REFERENCES `talkgroups` (`id`),
  CONSTRAINT `fk_users_repeaters` FOREIGN KEY (`owner_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `scheduled_nets` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `talkgroup_id` bigint(20) unsigned NOT NULL,
  `created_by_user_id` bigint(20) unsigned DEFAULT NULL,
  `name` longtext DEFAULT NULL,
  `description` longtext DEFAULT NULL,
  `cron_expression` longtext DEFAULT NULL,
  `day_of_week` bigint(20) DEFAULT NULL,
  `time_of_day` longtext DEFAULT NULL,
  `timezone` longtext DEFAULT NULL,
  `duration_minutes` bigint(20) unsigned DEFAULT NULL,
  `enabled` tinyint(1) DEFAULT 1,
  `next_run` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  `showcase` tinyint(1) DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_scheduled_nets_talkgroup_id` (`talkgroup_id`),
  KEY `idx_scheduled_nets_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `talkgroups` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `name` longtext DEFAULT NULL,
  `description` longtext DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_talkgroups_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

CREATE TABLE `users` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `callsign` longtext DEFAULT NULL,
  `username` longtext DEFAULT NULL,
  `password` longtext DEFAULT NULL,
  `admin` tinyint(1) DEFAULT NULL,
  `super_admin` tinyint(1) DEFAULT NULL,
  `email` longtext DEFAULT NULL,
  `approved` tinyint(1) DEFAULT NULL,
  `suspended` tinyint(1) DEFAULT NULL,
  `sms_storage_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `sms_storage_since` datetime(3) DEFAULT NULL,
  `share_positions` tinyint(1) NOT NULL DEFAULT 0,
  `must_change_password` tinyint(1) NOT NULL DEFAULT 0,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_users_callsign` (`callsign`) USING HASH,
  UNIQUE KEY `idx_users_username` (`username`) USING HASH,
  KEY `idx_users_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

INSERT INTO migrations (id) VALUES ('202302242025');
INSERT INTO migrations (id) VALUES ('202311190252');
INSERT INTO migrations (id) VALUES ('202506150100');
INSERT INTO migrations (id) VALUES ('202602100435');
INSERT INTO migrations (id) VALUES ('202602100457');
INSERT INTO migrations (id) VALUES ('202602130100');
INSERT INTO migrations (id) VALUES ('202602160100');
INSERT INTO migrations (id) VALUES ('202602160200');
INSERT INTO migrations (id) VALUES ('202602160300');
INSERT INTO migrations (id) VALUES ('202602170100');
INSERT INTO migrations (id) VALUES ('202602170200');
INSERT INTO migrations (id) VALUES ('202602170300');
INSERT INTO migrations (id) VALUES ('202610221900');
INSERT INTO migrations (id) VALUES ('202610221910');
INSERT INTO migrations (id) VALUES ('202610222000');
INSERT INTO migrations (id) VALUES ('202610230000');
INSERT INTO migrations (id) VALUES ('202610231200');
INSERT INTO migrations (id) VALUES ('202610232100');
INSERT INTO migrations (id) VALUES ('202610240100');
INSERT INTO migrations (id) VALUES ('202610240200');
INSERT INTO migrations (id) VALUES ('202610241000');
INSERT INTO migrations (id) VALUES ('202610242000');
INSERT INTO migrations (id) VALUES ('202610242100');
INSERT INTO migrations (id) VALUES ('202610251000');
INSERT INTO migrations (id) VALUES ('202610251010');
INSERT INTO migrations (id) VALUES ('202610251100');
INSERT INTO migrations (id) VALUES ('202610261000');
INSERT INTO migrations (id) VALUES ('202610261100');
INSERT INTO migrations (id) VALUES ('202610261200');
