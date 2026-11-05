# Hillshade test tiles

Each file is one 256 x 256 tile of the hillshade archive that `src/lib/map/archive.ts` names as `TERRAIN_URL` (`terrain/7c94bc6966ab-20260829`). The tile was decoded from its lossy WebP with libwebp, and the file holds the red channel, one byte per pixel in row order, compressed with zstd.

| File | Tile | Place |
| --- | --- | --- |
| `z12-656-1582.grey.zst` | z12/656/1582 | Oakland Estuary |
| `z12-654-1582.grey.zst` | z12/654/1582 | Golden Gate |
| `z11-172-596.grey.zst` | z11/172/596 | Kenai Fjords |
| `z11-170-599.grey.zst` | z11/170/599 | Kenai Fjords, open water |
| `z12-2128-1140.grey.zst` | z12/2128/1140 | Geirangerfjord |

The tiles are a derived product of the Copernicus DEM GLO-30 (free and open license):

produced using Copernicus WorldDEM-30 © DLR e.V. 2010-2014 and © Airbus Defence and Space GmbH 2014-2018 provided under COPERNICUS by the European Union and ESA; all rights reserved

The organisations in charge of the Copernicus programme by law or by delegation do not incur any liability for any use of the Copernicus WorldDEM-30.
