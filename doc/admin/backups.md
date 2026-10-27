# Back up and restore

A backup of DMRHub has two parts: the database, and the configuration of the deployment (the configuration file or the environment variables). Back up both, and keep the backups private. They hold the passwords of the hotspots and the repeaters, the password hashes of the users, and the `secret` and the `password-salt`. The database holds the Setup settings, and the `secret` and the `password-salt` when DMRHub made them.

Without the same `password-salt`, no user can log in, because DMRHub uses it to hash the passwords. Without the same `secret` and `password-salt`, the sessions and the app tokens stop working.

## SQLite in Docker

The commands below use the volume and the container of [Install DMRHub](install.md#docker), and zstd to compress the backup.

To make a backup, stop DMRHub, copy the volume, and start DMRHub again:

```bash
docker stop dmrhub
docker run --rm -v dmrhub-data:/data alpine tar -C /data -cf - . | zstd -o dmrhub-backup.tar.zst
docker start dmrhub
```

The backup has `DMRHub.db`, `DMRHub.db-wal` and `DMRHub.db-shm`. Keep the three database files together: the `-wal` file can hold the newest changes.

To restore a backup, stop DMRHub, empty the volume, unpack the backup into it, and start DMRHub:

```bash
docker stop dmrhub
docker run --rm -v dmrhub-data:/data alpine sh -c 'rm -rf /data/* /data/.[!.]*'
zstd -dc dmrhub-backup.tar.zst | docker run --rm -i -v dmrhub-data:/data alpine tar -C /data -xpf -
docker start dmrhub
```

`tar -p` keeps the owner of the files, user 65534.

## SQLite with systemd

With the unit of [Install DMRHub](install.md#run-dmrhub-with-systemd), the data is in `/etc/dmrhub`:

```bash
sudo systemctl stop dmrhub.service
sudo tar -C /etc/dmrhub -cf - . | zstd -o dmrhub-backup.tar.zst
sudo systemctl start dmrhub.service
```

To restore, stop DMRHub, unpack the backup into `/etc/dmrhub` with `zstd -dc dmrhub-backup.tar.zst | sudo tar -C /etc/dmrhub -xpf -`, and start DMRHub.

## PostgreSQL and MySQL

Use the backup tools of the database while DMRHub runs, for example:

```bash
pg_dump -Fc -h <host> -U dmrhub dmrhub > dmrhub.pgdump
mysqldump --single-transaction -h <host> -u dmrhub -p dmrhub | zstd -o dmrhub.sql.zst
```

Also back up the configuration file, or the environment variables, of each replica.

## Before an upgrade

Make a backup before each upgrade. A new release can change the database at its first start, and an older release cannot undo the change. See [Upgrade DMRHub](upgrade.md#roll-back).
