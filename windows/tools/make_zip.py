"""打包 zip。Python 会给中文文件名打上 UTF-8 标记，Windows 解压时不会乱码（macOS 自带 zip 命令不会）。
用法: python3 -I make_zip.py out.zip 文件...
"""
import os, sys, zipfile

out, files = sys.argv[1], sys.argv[2:]
with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    for f in files:
        z.write(f, os.path.basename(f))
