# Fixed upstream source

`mihomo-v1.19.32.tar.gz` is the unmodified corresponding upstream release
source from https://github.com/MetaCubeX/mihomo/tree/v1.19.32 .

SHA256: `ab130b7fab3893d01aa44c0d39be34a26da716c1d36832fbac9fa054a1d862a0`.

The shipped `clash_meta` ELF is v1.19.32, linux/amd64, with_gvisor, Go 1.26.8.
SHA256: `7a0d59da2e678d56c899a3db996a2ad8963286c4f3634b0451435db248f13fa1`.

This public source can contain upstream test fixtures; it contains no local
device state. Mihomo's build dependencies and instructions remain in its
upstream source. The slim UA3F-derived core lives directly under `core/`;
OpenClash's retained MIT engine files are under the firmware package, based on
commit e553e82e01c697aefbe1555261a503f425e00ccd. Modifications are documented in
the top-level NOTICE. All original license notices are retained.
