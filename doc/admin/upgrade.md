# Upgrade DMRHub

This page tells how to upgrade DMRHub, what DMRHub does to the database when it starts, and how to roll back.

## Before you upgrade

1. Read the release notes of each release between your version and the new one, on [GitHub Releases](https://github.com/USA-RedDragon/DMRHub/releases). To see your version, look at the first line of the log, for example `DMRHub - 1.3.16 (77d9c9f9...)`, or open `<siteurl>/api/v1/version`.
2. Make a backup. See [Back up and restore](backups.md).

## Docker

1. Pull the new image:

   ```bash
   docker pull ghcr.io/usa-reddragon/dmrhub:v1
   ```

2. Stop and remove the old container. The data stays in the volume:

   ```bash
   docker stop dmrhub
   docker rm dmrhub
   ```

3. Start a new container with the same `docker run` command as before, with the same volume and the same environment variables (see [Install DMRHub](install.md#docker)).
4. Check the log with `docker logs dmrhub`, then log in.

To go to one exact release, use its tag, for example `ghcr.io/usa-reddragon/dmrhub:v1.3.16`.

## Binary with systemd

1. Download the new `.tar.gz` file and unpack `DMRHub` from it.
2. Stop DMRHub, replace the program, and start DMRHub:

   ```bash
   sudo systemctl stop dmrhub.service
   sudo install -m 0755 DMRHub /usr/local/bin/DMRHub
   sudo systemctl start dmrhub.service
   ```

3. Check the log with `journalctl -u dmrhub.service`, then log in.

## What DMRHub does when it starts

At each start, before it opens the network, DMRHub brings the database up to date:

1. It runs each migration that the database did not get yet, in order. The `migrations` table lists the IDs of the migrations that ran. A migration can add or drop tables and columns, and change data. For example, the migrations from v1.3.0 to v1.3.16 add the plugin tables and the password reset table, drop the `app_settings` table, and delete the parrot user and talkgroup 9990 that older releases made, because the parrot is now a plugin.
2. It adds the tables and the columns that the current release needs and that are not in the database yet. This step does not drop anything.

With PostgreSQL or MySQL, DMRHub takes a lock in the database for these steps, so only one replica migrates at a time. If a migration fails, DMRHub exits with `failed to connect to database: failed to migrate database: ...`. Restore your backup and report the error.

## Several replicas

Upgrade one replica at a time. Each replica that stops hands its repeaters, peers, browser radios and plugins over to the other replicas within `dmr.drain-timeout`, and then waits for its calls to end. See [Replicas and scaling](replicas.md#stop-a-replica).

The first replica of the new release migrates the database. The replicas of the old release keep running on the migrated database until you upgrade them. Read the release notes for changes that the old release cannot run with.

## Roll back

DMRHub has no command to undo a migration. An older release ignores the migrations that it does not know, so it can start on a database that a newer release migrated. But the database is not as the older release left it, and some changes stay. For example, after an upgrade from v1.3.0 and a downgrade to v1.3.0 on the same database, v1.3.0 made the parrot user and talkgroup 9990 again. When we upgraded again, the migration that deletes them did not run a second time, so they stayed.

To roll back, restore the backup that you made before the upgrade, then start the older release. You lose the changes and the traffic records since the backup.

## What we tested

We tested these upgrades with Docker and SQLite on 2026-10-26, with the commands on this page and on [Back up and restore](backups.md):

| From | To | Data before the upgrade | Result |
| ---- | -- | ----------------------- | ------ |
| v1.3.15 (`ghcr.io/usa-reddragon/dmrhub:v1.3.15`) | main at `4b12a3d8`, built locally | An admin and a second user, talkgroup 91, MMDVM repeater 310001, one voice call in Last Heard | All data stayed. Both users logged in. A new call from the repeater showed in Last Heard. v1.3.15 and this main have the same 29 migrations, so no migration ran. |
| v1.3.0 (`ghcr.io/usa-reddragon/dmrhub:v1.3.0`) | main at `d7608e17`, built locally | The same data | 11 migrations ran (18 to 29 in the `migrations` table). All data stayed, and the parrot user and talkgroup 9990 of v1.3.0 were deleted. Both users logged in, and a new call showed in Last Heard. |
| main at `d7608e17` | v1.3.0, on the migrated database | The data above | v1.3.0 started, and all data showed. It made the parrot user and talkgroup 9990 again, and they stayed after the next upgrade. |
| main at `d7608e17` | v1.3.0, from the backup | The backup from before the upgrade | v1.3.0 started with the data of the backup, without the call made after the upgrade. |
