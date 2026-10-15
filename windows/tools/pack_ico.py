"""把多个 PNG 打包成 .ico（Vista 起支持 ICO 内直接嵌 PNG）。用法: python3 -I pack_ico.py out.ico a.png b.png ..."""
import struct, sys

out, pngs = sys.argv[1], sys.argv[2:]
blobs = [open(p, 'rb').read() for p in pngs]
header = struct.pack('<HHH', 0, 1, len(blobs))
offset = 6 + 16 * len(blobs)
entries = b''
for b in blobs:
    w, h = struct.unpack('>II', b[16:24])  # PNG IHDR 宽高
    entries += struct.pack('<BBBBHHII', w % 256, h % 256, 0, 0, 1, 32, len(b), offset)
    offset += len(b)
open(out, 'wb').write(header + entries + b''.join(blobs))
