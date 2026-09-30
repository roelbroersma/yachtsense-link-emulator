"""Render real read-only assets with synthetic telemetry; never contact a router."""
from pathlib import Path
import json
import os
import shutil
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[1]
ASSETS=ROOT/'cmd/yachtsense-link-emulator/dashboard'
fixture={"ok":True,"ready":True,"stale":False,"emulator":{"running":True,"version":(ROOT/"VERSION").read_text().strip(),"raynet":"eth0.3","cidr":"198.18.0.1/21","axiom":"198.18.3.234","discovery":"builtin","relay":True},"router":{"profile":"onWiFi","firmware":"RUTX_R_00.07.25.3","observed_at":"2026-09-30T17:45:00Z","internet":{"state":"active","links":[{"name":"wan1","kind":"wifi","label":"Harbour WiFi","rssi":-61,"ip":"10.171.1.86","state":"active","share":100},{"name":"mob1s1a1","kind":"mobile","label":"KPN","rssi":-77,"state":"standby","present":True,"sim":"1"},{"name":"wan-disabled","kind":"ethernet","disabled":True,"state":"offline"},{"name":"no-sim","kind":"mobile","present":False,"state":"offline"}]},"gps":{"state":"fix","latitude":52.205592,"longitude":5.081144,"satellites":12,"fences":[{"name":"InWetterwille","state":"inside"}]},"wifi":[{"name":"wlan1-2","ssid":"Boordwifi","band":"5 GHz","channel":36,"clients":3,"up":True},{"name":"wlan0-2","ssid":"Boordwifi IoT","band":"2.4 GHz","channel":6,"clients":5,"up":True}],"vpn":[{"name":"Kerklaan","kind":"IPsec","state":"connected","uptime":9240,"rx_bytes":74884513,"tx_bytes":1587448}],"rms":"connected","vxlan":[{"name":"vxlan1","up":True,"vni":404,"rx_bytes":2858311,"tx_bytes":954685}],"devices":[{"name":"Axiom","ip":"198.18.3.234","connection":"RayNet","state":"observed"},{"name":"Cerbo GX","ip":"198.18.5.98","connection":"RayNet","state":"reachable"},{"name":"Sonos","ip":"192.168.40.179","connection":"LAN","state":"reachable"},{"name":"iPhone","ip":"192.168.40.12","connection":"Wi-Fi","ssid":"Boordwifi","state":"associated"},{"name":"Static LAN device","ip":"192.168.40.30","connection":"LAN","state":"known"}],"logs":["17:45:00 Axiom/MFD detected — 198.18.3.234"]}}
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH') or shutil.which('chromium') or shutil.which('google-chrome'),headless=True,args=['--no-sandbox'])
 for name,width,height in [('desktop',1120,900),('axiom',800,480),('mobile',390,844)]:
  page=browser.new_page(viewport={'width':width,'height':height},device_scale_factor=1)
  current=json.loads(json.dumps(fixture))
  html=(ASSETS/'index.html').read_text().replace('<link rel="stylesheet" href="/status.css">','<style>'+(ASSETS/'status.css').read_text()+'</style>').replace('<script src="/status.js" defer></script>','')
  page.set_content(html)
  page.evaluate('window.__fixture='+json.dumps(current))
  page.add_script_tag(content='window.XMLHttpRequest=function(){this.open=function(){};this.send=function(){this.readyState=4;this.status=200;this.responseText=JSON.stringify(window.__fixture);this.onreadystatechange();};};')
  page.add_script_tag(content=(ASSETS/'status.js').read_text())
  page.wait_for_selector('#content:not([hidden])')
  text=page.locator('body').inner_text()
  for unwanted in ['Selected link','Your connection','Live router status','default IPv4 policy','50%','reported by','peer reachability','Marina WiFi','Built-in relay','Avahi','wan-disabled','no-sim','Public IP']:
   assert unwanted.lower() not in text.lower(),(name,unwanted)
  assert 'Kerklaan · IPsec' in text and 'onWiFi' in text and 'GPS' in text
  assert page.locator('#devices tr').count()==5
  assert page.get_by_role('heading',name='RayNet',exact=True).count()==0
  assert page.locator('#services .row-title').all_text_contents()==['RayNet','RMS','vxlan1 · VXLAN']
  assert 'eth0.3 · 198.18.0.1/21' in page.locator('#services').inner_text()
  assert 'Axiom' not in page.locator('#services').inner_text()
  for device in ['Axiom','Cerbo GX','Sonos','iPhone','Static LAN device']:
   assert page.locator('#devices').get_by_text(device,exact=True).count()==1
  assert not page.evaluate('document.documentElement.scrollWidth>window.innerWidth'),name
  (ROOT/'build').mkdir(exist_ok=True)
  page.screenshot(path=str(ROOT/'build'/('dashboard-'+name+'.png')),full_page=True)
  page.locator('#search').fill('Static');assert page.locator('#devices tr').count()==1
  # Untrusted device names must be rendered as text, never HTML.
  current['router']['devices'][0]['name']='<img src=x onerror="window.bad=true">'
  current['router']['internet']['links'][0]['public_ip']='8.8.8.8'
  page.evaluate('window.__fixture='+json.dumps(current))
  page.locator('#search').fill('');page.wait_for_timeout(5200)
  assert not page.evaluate('Boolean(window.bad)')
  assert page.get_by_text('Public IP',exact=True).count()==1
  assert page.locator('#devices img').count()==0
  page.close();print('PASS',name,'compact page, conditional fields, devices, text-safe rendering')
 browser.close()
