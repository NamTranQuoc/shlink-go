<?php
// Rerun from the repo root: docker run --rm --entrypoint php -v "$PWD/internal/crawler/testdata":/work shlinkio/shlink:5.1.8 /work/gen/gen.php [extra UA sources mounted under /work...]

// Writes /work/golden.json (or $GOLDEN_OUT): every user agent of the existing golden file, plus the
// built-in edge cases below, plus extra sources, each paired with CrawlerDetect::isCrawler() as
// Shlink calls it. A source is a text file with one UA per line ("file.txt@N" keeps every Nth line)
// or a phpuseragentparser user_agents JSON file (UAs are the keys).
//
// The committed corpus was built from crawler-detect's tests/data/user_agent/crawlers.txt,
// tests/data/user_agent/devices.txt@16 and tests/data/sec_ch_ua/*.txt (commit d4b03a96, v1.4.2) and
// phpuseragentparser's tests/user_agents.dist.json (v1.12.0).

declare(strict_types=1);

require '/etc/shlink/vendor/autoload.php';

ini_set('memory_limit', '-1');

use Jaybizzle\CrawlerDetect\CrawlerDetect;

$out = getenv('GOLDEN_OUT') ?: '/work/golden.json';

/** @return list<string> */
function decodeCases(array $cases): array
{
    return array_map(static fn (array $c) => isset($c['b64']) ? base64_decode($c['b64'], true) : $c['ua'], $cases);
}

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
    $uas = decodeCases(json_decode(file_get_contents('/work/golden.json'), true, flags: JSON_THROW_ON_ERROR));
}

// Shlink test constants (config/test/constants.php) and common browsers.
$extras = [
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
    'Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)',
    'Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)',
    'curl/8.5.0', 'Wget/1.21.4', 'python-requests/2.31.0', 'Go-http-client/1.1', 'okhttp/4.12.0',
    'Shlink', 'shlink-go', 'PostmanRuntime/7.36.0', 'facebookexternalhit/1.1', 'Slackbot-LinkExpanding 1.0',
    // Empty and whitespace (Shlink passes "" when there is no User-Agent header).
    '', '0', '00', ' ', "\t", "\n", " \t\n\r\0\x0B", ' 0 ',
    // Anchors: PCRE "$" also matches before a final "\n"; isCrawler trims the agent first.
    'b0t', 'b0t ', "b0t\n", "b0t\n\n", ' b0t', 'xb0t', 'B0T', "b0t\nx",
    'Amazon Simple Notification Service Agent', "Amazon Simple Notification Service Agent\n", 'Amazon Simple Notification Service Agentx',
    'DebugBear', 'DebugBearx', 'xxDebugBear', ' Aether', 'Aether', 'aether/1', 'Mozilla/5.0 Aether',
    'Ruby', 'xRuby', 'xRuby/2', 'ruby/', "\nRuby", "\x0BRuby",
    // Negative lookahead Yandex(?!Search).
    'Yandex', 'yandex', 'YandexSearch', 'yandexsearch', 'YANDEXSEARCH/1.0', 'YandexSearc', 'YandexSearchYandex',
    'YandexSearch Yandex', 'xYandexSearch', 'YaYandexSearch', 'YandexYandexSearch', 'Mozilla/5.0 (Linux; Android 10) YandexSearch/8.10 Mobile Safari/537.36',
    'Mozilla/5.0 (Linux; Android 10) YaBrowser/23.1 Yandex Mobile Safari', '"Yandex"', 'YandexModule2', 'YandexModule2 Yandex',
    // Exclusions are removed before matching, which can fuse the text around them.
    'GooSafari/1.2.3gle', 'b0Safari/1t', 'bSafari/1.0ot', 'cubot', 'Cubot Note 20', 'Mozilla/5.0 (Linux; Android 11; cubot X30)',
    '; M bot', '; m BOT', 'x; M bot', '; M bots', 'htc_botdugls', 'super/12/Android/9', 'Opera Mini/12.34.5/36.', 'Opera',
    'x86_64 bot', 'x86_éé', 'x86_ébot', 'x86_ b', 'Linuxbot', 'Mac OS X 10_15_7bot', 'like Geckobot', '(like )Gecko/2010',
    'CPU iPhone OS 16_2 like Mac OS X', 'CPU OS 1_ like Mac OS X', ' Intel bot', 'Name Intelligence', ' .NET CLR 2.0.50727', ' .NET4.0C',
    // Byte semantics: PCRE runs without /u, Go regexp decodes UTF-8 and folds Unicode.
    "\u{212A}elny/1.0", 'Kelny/1.0', "\u{017F}lack/1", 'Slack/1', "\u{017F}cubot", 'bötü', "b\u{00F6}t", 'BOT', 'Bot',
    "Mozilla/5.0 (Windows NT 10.0) \xFF\xFEbot", "\xC3bot", "\xE2\x80bot", "\xC3", "\x80\x81\x82", "Safari/1\xC3\xA9", "x86_\xC3\xA9bot",
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0 Safari/537.36 测试', 'Яндекс бот', 'Яндекс', 'ẞot',
    "Mozilla/5.0\0bot", "\0", "Mozilla/5.0 (compatible; Bot\x0B)", 'centuryb.o.t9[at]gmail.com', 'centurybXoYt9tgmailXcom',
    'Zend\\Http\\Client', 'Zend\\\\Http\\\\Client', 'Zend_Http_Client', 'WhereGoes?', 'Daum/4', 'Daumoa 4', 'daumoa/', 'Y!J-ABC', 'Y!J-AB',
    'xpymep.exe', 'xpymep1.exe', 'Sitemap Generator', 'Sitemaps Generator', 'HAAARTLAND http client', 'HAARTLAND http client',
    'Project 25499', 'Project25499', 'Go 1.21 package http', 'Go  package http', 'PTST/1', 'PTST/', 'Java/1.8 outbrain', 'Java outbrain',
    'NewsBlur Feed Finder', 'NewsBlurFinder',
    // "." and "$" around carriage returns and newlines.
    "Java\routbrain", "Java\noutbrain", "NewsBlur \r Finder", "centuryb\ro\rt9agmail\rcom", "b0t\r", "Ruby\r", "x86_\r\n", "x86_\rbot",
];
// Very long strings (Shlink does not crop the UA before isCrawler).
foreach ([100, 1000, 5000, 20000] as $n) {
    $extras[] = str_repeat('a', $n);
    $extras[] = str_repeat('a', $n) . 'bot';
    $extras[] = 'bot' . str_repeat('a', $n);
    $extras[] = str_repeat('a', $n) . ' Googlebot';
    $extras[] = str_repeat('Mozilla/5.0 ', intdiv($n, 12) + 1);
    $extras[] = str_repeat('ab ', intdiv($n, 3) + 1) . 'crawl';
    $extras[] = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) ' . str_repeat('x', $n) . ' Safari/537.36';
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

$detector = new CrawlerDetect(); // Shlink keeps one static instance, see isCrawler() in module/Core/functions/functions.php
$cases = [];
foreach ($uas as $ua) {
    // JSON strings must be valid UTF-8; other byte strings are stored base64-encoded.
    $case = json_encode($ua) !== false ? ['ua' => $ua] : ['b64' => base64_encode($ua)];
    $case['crawler'] = $detector->isCrawler($ua);
    $cases[] = $case;
}

$lines = array_map(static fn (array $c) => json_encode($c, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR), $cases);
file_put_contents($out, "[\n" . implode(",\n", $lines) . "\n]\n");
fwrite(STDERR, sprintf("%d cases, %d crawlers -> %s\n", count($cases), count(array_filter(array_column($cases, 'crawler'))), $out));
