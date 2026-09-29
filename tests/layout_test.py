"""Render UI fixtures locally; this is not a real RutOS browser session."""
from pathlib import Path
from playwright.sync_api import sync_playwright
import re
root = Path(__file__).resolve().parents[1]
css = (root/'package/root/www/assets/yachtsense-link-emulator-v1120.css').read_text()
with sync_playwright() as p:
    browser = p.chromium.launch(executable_path='/usr/bin/chromium', headless=True, args=['--no-sandbox'])
    for name, width, height in [('desktop',1280,1050),('mobile',390,844),('editor',1280,1050)]:
        page = browser.new_page(viewport={'width':width,'height':height}, device_scale_factor=1)
        src = 'preview-editor.html' if name=='editor' else 'preview.html'
        html = re.sub(r'<link rel="stylesheet"[^>]*>', '<style>'+css+'</style>', (root/'build'/src).read_text())
        page.set_content(html)
        page.wait_for_timeout(100)
        assert not page.evaluate('document.documentElement.scrollWidth > innerWidth'), name+' horizontal overflow'
        assert page.locator('details[open]').count() == 0
        if name == 'editor':
            assert page.get_by_role('region',name='Unsaved settings').count()==1
        page.screenshot(path=str(root/'build'/f'{name}.png'), full_page=True)
        page.locator('summary').first.click()
        assert page.locator('details[open]').count()==1
        assert not page.evaluate('document.documentElement.scrollWidth > innerWidth')
        print('PASS '+name+': no horizontal overflow; expandable Advanced; screenshot inspected separately')
        page.close()
    browser.close()
