from argparse import ArgumentParser
from pathlib import Path
import hashlib
import os
import shutil
import subprocess
import tempfile
import xml.etree.ElementTree as ET

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parent
INK = '#202725'
PAPER = '#F5F2EB'
NAMESPACE = '{http://www.w3.org/2000/svg}'
ET.register_namespace('', 'http://www.w3.org/2000/svg')


def drawing(symbol=False, color='currentColor'):
    root = ET.parse(ROOT / 'source/gofastr.svg').getroot()
    root.set('fill', color)
    if symbol:
        root.remove(root.find(f"{NAMESPACE}g[@id='fastr']"))
    return root


def icon_drawing():
    icon = ET.Element(f'{NAMESPACE}svg', {'viewBox': '0 0 512 512'})
    ET.SubElement(icon, f'{NAMESPACE}rect', {'width': '512', 'height': '512', 'rx': '96', 'fill': INK})
    symbol = drawing(True, PAPER).find(f'{NAMESPACE}g')
    symbol.set('fill', PAPER)
    symbol.set('transform', 'translate(56 129) scale(1.4)')
    icon.append(symbol)
    return icon


# Each raster and the file it was made from. MANIFEST records both hashes
# once generate() finishes, so --check catches a raster left behind by a
# run that rewrote its source and then failed.
DERIVED = [
    *[(f'gofastr-{label}-{scheme}.png', f'gofastr-{label}-{scheme}.svg')
      for label in ['logo', 'symbol'] for scheme in ['light', 'dark']],
    ('gofastr-icon.png', 'gofastr-icon.svg'),
    ('gofastr-favicon.ico', 'gofastr-icon.png'),
    *[(f'gofastr-preview-{scheme}.png', f'gofastr-logo-{scheme}.png') for scheme in ['light', 'dark']],
]
MANIFEST = ROOT / 'renders.sha256'


def digest(name):
    return hashlib.sha256((ROOT / name).read_bytes()).hexdigest()


def manifest_lines():
    return [f'{digest(source)} {digest(output)} {output}' for output, source in DERIVED]


def write_svg(name, root):
    ET.ElementTree(root).write(ROOT / name, encoding='unicode')


def render(chrome, source, destination, width, height):
    with tempfile.TemporaryDirectory(prefix='gofastr-brand-') as profile:
        subprocess.run([
            chrome, '--headless', '--disable-gpu', '--hide-scrollbars',
            '--no-first-run', '--no-default-browser-check',
            '--default-background-color=00000000', f'--user-data-dir={profile}',
            f'--window-size={width},{height}', f'--screenshot={destination}',
            source.as_uri(),
        ], check=True, capture_output=True, timeout=40)


def previews():
    panels = []
    for scheme, background, foreground in [('light', PAPER, INK), ('dark', INK, PAPER)]:
        panel = Image.new('RGB', (1000, 760), background)
        logo = Image.open(ROOT / f'gofastr-logo-{scheme}.png').convert('RGBA')
        logo.thumbnail((760, 530), Image.Resampling.LANCZOS)
        panel.paste(logo, ((1000 - logo.width) // 2, (760 - logo.height) // 2), logo)
        ImageDraw.Draw(panel).text((36, 28), scheme.upper(), fill=foreground, font=ImageFont.load_default(size=18))
        panel.save(ROOT / f'gofastr-preview-{scheme}.png')
        panels.append(panel)
    board = Image.new('RGB', (2000, 760))
    for index, panel in enumerate(panels):
        board.paste(panel, (1000 * index, 0))
    board.save(ROOT / 'gofastr-preview.png')


def generate():
    chrome = os.environ.get('CHROME_BIN') or shutil.which('google-chrome') or shutil.which('chromium')
    if not chrome:
        candidate = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
        chrome = candidate if Path(candidate).exists() else None
    if not chrome:
        raise RuntimeError('Set CHROME_BIN to a Chrome or Chromium executable.')
    for symbol, label in [(False, 'logo'), (True, 'symbol')]:
        write_svg(f'gofastr-{label}.svg', drawing(symbol))
        for scheme, color in [('light', INK), ('dark', PAPER)]:
            name = f'gofastr-{label}-{scheme}'
            write_svg(f'{name}.svg', drawing(symbol, color))
            render(chrome, ROOT / f'{name}.svg', ROOT / f'{name}.png', 1585, 1065)
    shutil.copyfile(ROOT / 'gofastr-logo-light.png', ROOT / 'gofastr-logo.png')
    write_svg('gofastr-icon.svg', icon_drawing())
    render(chrome, ROOT / 'gofastr-icon.svg', ROOT / 'gofastr-icon.png', 512, 512)
    with Image.open(ROOT / 'gofastr-icon.png') as image:
        image.save(ROOT / 'gofastr-favicon.ico', sizes=[(16, 16), (32, 32), (48, 48)])
    previews()
    MANIFEST.write_text('\n'.join(manifest_lines()) + '\n')


def validate():
    for symbol, label in [(False, 'logo'), (True, 'symbol')]:
        for suffix, color in [('', 'currentColor'), ('-light', INK), ('-dark', PAPER)]:
            expected = ET.tostring(drawing(symbol, color), encoding='unicode')
            assert (ROOT / f'gofastr-{label}{suffix}.svg').read_text() == expected
    assert (ROOT / 'gofastr-icon.svg').read_text() == ET.tostring(icon_drawing(), encoding='unicode')
    assert (ROOT / 'gofastr-logo.png').read_bytes() == (ROOT / 'gofastr-logo-light.png').read_bytes()
    assert MANIFEST.read_text().splitlines() == manifest_lines(), \
        'a raster no longer matches the source it was rendered from; run generate.py'
    for path in [ROOT / 'source/gofastr.svg', *ROOT.glob('*.svg')]:
        root = ET.parse(path).getroot()
        assert root.tag == f'{NAMESPACE}svg', path
        assert root.findall(f'.//{NAMESPACE}path'), path
        assert not root.findall(f'.//{NAMESPACE}image'), path
        assert not root.findall(f'.//{NAMESPACE}text'), path
    for label in ['logo', 'symbol']:
        with Image.open(ROOT / f'gofastr-{label}-light.png') as light, Image.open(ROOT / f'gofastr-{label}-dark.png') as dark:
            assert light.size == dark.size == (1585, 1065)
            assert light.getchannel('A').tobytes() == dark.getchannel('A').tobytes()
            alpha = light.getchannel('A')
            assert alpha.getextrema() == (0, 255)
            left, top, right, bottom = alpha.getbbox()
            assert 0 < left < right < light.width and 0 < top < bottom < light.height
    with Image.open(ROOT / 'gofastr-icon.png') as icon:
        assert icon.size == (512, 512)
        assert icon.getpixel((256, 256))[3] == 255
    with Image.open(ROOT / 'gofastr-favicon.ico') as icon:
        assert icon.ico.sizes() == {(16, 16), (32, 32), (48, 48)}
    print('PASS: outlined SVGs, rasters current with their sources, theme-matched transparent PNGs, square icon, and favicon sizes.')


if __name__ == '__main__':
    parser = ArgumentParser(description='Export and validate the approved GoFastr identity.')
    parser.add_argument('--check', action='store_true', help='Validate existing exports without rendering.')
    arguments = parser.parse_args()
    if not arguments.check:
        generate()
    validate()
