<?php
// Rerun from the repo root: docker run --rm --entrypoint php -v "$PWD/internal/useragent/testdata":/work shlinkio/shlink:5.1.8 /work/gen/gen.php [extra UA sources mounted under /work...]

// Writes /work/golden.json (or $GOLDEN_OUT): every user agent of the existing golden file, plus the
// built-in edge cases below, plus extra sources, each paired with UserAgentParser::parse() as Shlink
// calls it and with Shlink's DeviceType::matchFromUserAgent() and Browser::matchFromUserAgent().
// A source is a text file with one UA per line ("file.txt@N" keeps every Nth line) or a
// phpuseragentparser user_agents JSON file (UAs are the keys).
//
// The committed corpus was built from crawler-detect's tests/data/user_agent/crawlers.txt,
// tests/data/user_agent/devices.txt@16 and tests/data/sec_ch_ua/*.txt (commit d4b03a96, v1.4.2) and
// phpuseragentparser's tests/user_agents.dist.json (v1.12.0).

declare(strict_types=1);

require '/etc/shlink/vendor/autoload.php';

ini_set('memory_limit', '-1');

use donatj\UserAgent\UserAgentParser;
use Shlinkio\Shlink\Core\Model\Browser;
use Shlinkio\Shlink\Core\Model\DeviceType;

$out = getenv('GOLDEN_OUT') ?: '/work/golden.json';

/** @return list<string> */
function readSource(string $arg): array
{
    [$file, $step] = array_pad(explode('@', $arg, 2), 2, '1');
    $step = max(1, (int) $step);
    if (str_ends_with($file, '.json')) {
        return array_map('strval', array_keys(json_decode(file_get_contents($file), true, flags: JSON_THROW_ON_ERROR)));
    }
    $lines = explode("\n", rtrim(file_get_contents($file), "\n"));
    return array_values(array_filter($lines, static fn (int $i) => $i % $step === 0, ARRAY_FILTER_USE_KEY));
}

$uas = [];
if (is_file('/work/golden.json')) {
    foreach (json_decode(file_get_contents('/work/golden.json'), true, flags: JSON_THROW_ON_ERROR) as $c) {
        $uas[] = isset($c['b64']) ? base64_decode($c['b64'], true) : $c['ua'];
    }
}

$extras = [
    // Shlink test constants (config/test/constants.php) and common browsers.
    'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.5414.86 Mobile Safari/537.36',
    'Mozilla/5.0 (iPhone; CPU iPhone OS 16_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/109.0 Mobile/15E148 Safari/605.1.15',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 Edg/138.0.3351.95',
    'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/81.0.4044.113 Safari/537.36',
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.4 Safari/605.1.15',
    'Mozilla/5.0 (X11; CrOS x86_64 16181.61.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.6998.198 Safari/537.36',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.3856.62',
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_7_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 OPR/128.0.0.0',
    'Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36',
    'Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
    'Mozilla/5.0 (Linux; U; Android 4.0.3; en-us) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30',
    'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 EdgA/120.0.0.0',
    'Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)', 'curl/8.5.0', 'Wget/1.21.4', 'python-requests/2.31.0',
    // Empty, falsy and whitespace.
    '', '0', '00', ' ', "\t", "\n", '0.0', 'a', '-', '/', '(', ')', '()', '(;)', '( ; )',
    // Branches and their edge cases.
    'Mozilla/5.0 (Windows NT 10.0; rv:0) like Gecko Trident/7.0', 'Mozilla/5.0 (Windows NT 10.0; rv:11.0) like Gecko Trident/7.0',
    'Mozilla/5.0 (Windows NT 10.0; rv:00) like Gecko Trident/7.0', 'Mozilla/5.0 (X11; linux-gnu) Firefox/', 'Mozilla/5.0 (x11; linux) firefox/abc',
    'Mozilla/5.0 (Adr; adr 4) Chrome/1', 'Mozilla/5.0 (BB10; Touch) AppleWebKit/537.10+ (KHTML, like Gecko) Version/10.0.9.2372 Mobile Safari/537.10+',
    'Mozilla/5.0 (PlayBook; U; RIM Tablet OS 2.1.0; en-US) AppleWebKit/536.2+ (KHTML like Gecko) Version/7.2.1.0 Safari/536.2+',
    'Mozilla/5.0 (Linux; U; en-us; KFTT Build/IML74K) AppleWebKit/535.19 (KHTML, like Gecko) Silk/3.4 Safari/535.19 Silk-Accelerated=true',
    'Mozilla/5.0 (Linux; Kindle Fire) Silk/0 Version/1.2', 'Mozilla/5.0 (Kindle Fire) Kindle Fire/x Version/3', 'Mozilla/5.0 (Kindle Fire) Kindle Fire/ Silk/',
    'Mozilla/5.0 (Nintendo 3DS; U; ; en) Version/1.7412.EU', 'Mozilla/5.0 (New Nintendo 3DS like iPhone) AppleWebKit/536.30 (KHTML, like Gecko) NX/3.0.0.5.15 Mobile NintendoBrowser/1.3.10126.EU',
    'Mozilla/5.0 (PlayStation Vita 3.61) AppleWebKit/537.73 (KHTML, like Gecko) Silk/3.2', 'Mozilla/5.0 (PLAYSTATION 3 4.81) AppleWebKit/531.22.8 (KHTML, like Gecko)',
    'Mozilla/5.0 (PlayStation 4 9.00) AppleWebKit/605.1.15 (KHTML, like Gecko)', 'Mozilla/5.0 (PlayStation; PlayStation 5/2.26) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0 Safari/605.1.15',
    'Mozilla/5.0 (iPhone; CPU iPhone OS 9_3 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Mobile/13E233 Puffin/4.8.0.2965AP',
    'Mozilla/5.0 (X11; U; Linux x86_64; en-us) AppleWebKit/537.36 (KHTML, like Gecko) Puffin/7.7.0.40871WT', 'Puffin/1.2XY', 'Puffin/1.23', 'Mozilla/5.0 (X) Puffin/12345ab',
    'Mozilla/5.0 (Windows NT 6.1) Opera 12.10', 'Opera/9.80 (Windows NT 6.1; U; es-ES) Presto/2.9.181 Version/12.00', 'Opera/9.80 (S60; SymbOS; Opera Mobi/SYB-1107071606; U; en) Presto/2.8.149 Version/11.10',
    'Mozilla/5.0 (Windows; U; Windows NT 6.1) AppleWebKit/533.20.25 (KHTML, like Gecko) Version/5.0.4', 'Mozilla/5.0 (Tizen 2.3) AppleWebKit/537.3 (KHTML, like Gecko) TizenBrowser/2.3',
    'Mozilla/5.0 (Windows Phone 10.0; Android 6.0.1; Xbox; Xbox One) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/52.0.2743.116 Mobile Safari/537.36 Edge/15.15063',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.102 Safari/537.36 edge/18.18363',
    'Mozilla/5.0 (Windows NT 10.0) EDGE/18 Chrome/70', 'Mozilla/5.0 (Windows NT 10.0) Edg/x Chrome/70', 'Mozilla/5.0 (Windows NT 10.0) OPR/ Chrome/70',
    'Mozilla/5.0 (Windows NT 10.0) Chrome/1 YaBrowser/22.1', 'Mozilla/5.0 (Linux; Android 11) XiaoMi/MiuiBrowser/13.5.40', 'Mozilla/5.0 (Linux; Android 9) YaApp_Android/22.1',
    'Mozilla/5.0 (X11; Linux) Iceweasel/38 Firefox/38', 'Mozilla/5.0 (X11; Linux) IceCat/60', 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) CriOS/120.0 Mobile/15E148 Safari/604.1',
    'Mozilla/5.0 (Macintosh) Facebot/1', 'Mozilla/5.0 (Linux; Android 9) UCBrowser/12.1', 'Mozilla/5.0 (X11; FreeBSD amd64; OpenBSD; NetBSD) Firefox/1', 'Mozilla/5.0 (Sailfish 3.0; Mobile) SailfishBrowser/1.0',
    'Mozilla/5.0 (Fuchsia) Chrome/1', 'Mozilla/5.0 (SymbianOS/9.4; Series60/5.0) AppleWebKit/525', 'Mozilla/5.0 (Nintendo Switch; WifiWebAuthApplet) AppleWebKit/606.4 NF/6.0.1.15.4 NintendoBrowser/5.1.0.20393',
    'Mozilla/5.0 (Nintendo WiiU) AppleWebKit/536.30 NX/3.0.4.2.12 NintendoBrowser/4.3.1.11264.US', 'Mozilla/5.0 (New Nintendo DS) x', 'Mozilla/5.0 (Xbox One) y', 'Mozilla/5.0 (Windows; xbox) z',
    'Mozilla/5.0 (Valve Steam Tenfoot/1) Valve Steam Tenfoot/1', 'Mozilla/5.0 (X11) Mastodon/4.2.1 (http.rb/5.1.1; +https://mastodon.social/)', 'Lynx/2.8.9rel.1 libwww-FM/2.14',
    'Mozilla/5.0 (Windows NT 10.0) Whale/3.0 Vivaldi/6 Midori/9 OculusBrowser/1 SamsungBrowser/2', 'Mozilla/5.0 (compatible; YandexBot/3.0; +http://yandex.com/bots)',
    'Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)', 'Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.0; +https://openai.com/gptbot)',
    'Twitterbot/1.0', 'facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)', 'Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)',
    'Mozilla/5.0 (compatible; SemrushBot/7~bl; +http://www.semrush.com/bot.html)', 'Mozilla/5.0 (compatible; Foo robot; +mailto:a@b.c)', 'Mozilla/5.0 (compatible; Foo v0; +foo@bar.com)',
    'Mozilla/5.0 (compatible; Foo v0.0; +foo@bar.com)', 'x ( +http:', 'x (  +http://a', '(\v+http:', "(\f+http:", "(\v\v+https:", 'Bot 0 (+http://x)', 'Bot/0 (+a@b)',
    'Bot:0', 'Bot:00', 'Bot 0', 'Bot', 'MozillaBot/1', 'mozilla/1', 'Mozila/1', '-/1', 'a b c', 'a:b', 'ab/0x', 'Mozilla/5.0 (ab) Safari/', 'Mozilla/5.0 (ab) Safari/0',
    'Mozilla/5.0 (Linux; Android 4.4) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0', 'Mozilla/5.0 (Linux) Android/4.4 AppleWebKit/537.36', 'Mozilla/5.0 Android 4.4 AppleWebKit/537.36',
    'Mozilla/5.0 (Linux; android 9) applewebkit/537.36', 'Mozilla/5.0 (Windows; U; MSIE 9.0; Windows NT 9.0; en-US)', 'Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.1)',
    'Mozilla/5.0 (compatible; MSIE 10.0; Windows Phone 8.0; Trident/6.0; IEMobile/10.0; ARM; Touch; NOKIA; Lumia 920)', 'Camino/2.1', 'Mozilla/5.0 (Macintosh) Camino/2.1 (like Firefox/3.6)',
    // Newlines, carriage returns and other control bytes.
    "Mozilla/5.0 (Windows NT 10.0;\r Win64) Chrome/1", "Mozilla/5.0 (Windows\n NT) Chrome/1", "Mozilla/5.0 (X11\r) Firefox/1", "Mozilla/5.0 (Windows\r) Firefox/1",
    "\rChrome/1", "\nChrome/1", "x\rSafari/1", "Mozilla/5.0 (Linux)\nFirefox/1", "Bot/1\r(+http:", "Mozilla/5.0 (compatible;\x0BFoo/1; +http://x)",
    "Mozilla/5.0 (compatible;\xA0Foo/1;\xA0+http://x)", "Mozilla/5.0 (compatible;\x85Foo/1;\x85+http://x)", "Mozilla/5.0 (Foo\x00; +http://x)", "Mozilla/5.0 (\x00Foo\x00; +http://x)",
    // Byte semantics: PCRE runs without /u, Go regexp decodes UTF-8 and folds Unicode.
    "\u{017F}afari/1", "\u{212A}indle/1", "Mozilla/5.0 (\u{212A}indle Fire) Silk/1", "\u{017F}bot/1 (+http://x)", 'Mozilla/5.0 (compatible; Bötli/1.0; +http://x)',
    'Mozilla/5.0 (compatible; 测试机器人/1.0; +http://example.com)', "Mozilla/5.0 (compatible; \xFF\xFE/1; +http://x)", "Mozilla/5.0 (Windows NT 10.0) \xC3Chrome/1",
    "\xC3Chrome/1", "Mozilla/5.0 (Windows NT 10.0) é Chrome/1", "Mozilla/5.0 (Windows; \xE9) Firefox/1", "Mozilla/5.0 (\xC3; Android 9) Chrome/1", "\xC3\xA9/1", "\xC3/1 (+http:",
    "Mozilla/5.0 (Linux; Android 9; \u{212A}) Chrome/1", 'ÄÖÜ', '😀 Chrome/1', "Mozilla/5.0 (X11; Linux x86_64; rv:\u{0130}) Gecko Trident/7",
];
foreach ([1000, 5000, 20000] as $n) {
    $extras[] = str_repeat('a', $n);
    $extras[] = 'Mozilla/5.0 (Windows NT 10.0; ' . str_repeat('x', $n) . ') Chrome/1';
    $extras[] = str_repeat('Chrome/1 ', intdiv($n, 9));
    $extras[] = '(' . str_repeat('a', $n) . ' +http://x';
}
// Inputs that make backtracking engines work hard (PHP gives up past pcre.backtrack_limit).
$n = 8192;
array_push($extras,
    str_repeat('a', $n) . ' (+', '(a/' . str_repeat('1', $n) . 'x+', '(' . str_repeat(';', $n), str_repeat('(', $n),
    "outbrain\n" . str_repeat('java', intdiv($n, 4)), "Finder\n" . str_repeat('NewsBlur ', intdiv($n, 9)),
    str_repeat(' x', intdiv($n, 2)), '(' . str_repeat(' ', $n) . '+', 'Safari/' . str_repeat('1', $n),
    'Mozilla/5.0 (' . str_repeat('x; ', intdiv($n, 3)) . ') Chrome/1', '(' . str_repeat(' ', 2000) . '+',
    '(' . str_repeat(' ', 500) . '+', '(a/' . str_repeat('1', 1000) . 'x+', str_repeat('(', 1000) . ' +http:',
);
$uas = array_merge($uas, $extras);
foreach (array_slice($argv, 1) as $arg) {
    $uas = array_merge($uas, readSource($arg));
}
$uas = array_values(array_unique($uas, SORT_STRING));

$parser = new UserAgentParser(); // Shlink keeps one static instance, see parseUserAgent() in module/Core/functions/functions.php
$lines = [];
foreach ($uas as $ua) {
    // JSON strings must be valid UTF-8; other byte strings are stored base64-encoded.
    $case = json_encode($ua) !== false ? ['ua' => $ua] : ['b64' => base64_encode($ua)];
    $parsed = $parser->parse($ua);
    $case['platform'] = $parsed->platform();
    $case['browser'] = $parsed->browser();
    $case['version'] = $parsed->browserVersion();
    $case['devices'] = array_map(static fn (DeviceType $d) => $d->value, DeviceType::matchFromUserAgent($ua));
    $case['shlinkBrowser'] = Browser::matchFromUserAgent($ua)?->value;
    $line = json_encode($case, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    if ($line === false) {
        // Captured text can cut a multi-byte character; keep such results base64-encoded too.
        foreach (['platform', 'browser', 'version'] as $k) {
            if ($case[$k] !== null && json_encode($case[$k]) === false) {
                $case[$k . 'B64'] = base64_encode($case[$k]);
                $case[$k] = null;
            }
        }
        $line = json_encode($case, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    }
    $lines[] = $line;
}

file_put_contents($out, "[\n" . implode(",\n", $lines) . "\n]\n");
fwrite(STDERR, sprintf("%d cases -> %s\n", count($lines), $out));
