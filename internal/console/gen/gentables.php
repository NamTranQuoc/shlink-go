<?php
$wide = require '/etc/shlink/vendor/symfony/string/Resources/data/wcswidth_table_wide.php';
$zero = require '/etc/shlink/vendor/symfony/string/Resources/data/wcswidth_table_zero.php';
function emit($name, $t) {
    echo "var $name = [][2]rune{\n";
    $line = "\t";
    foreach ($t as [$a, $b]) {
        $item = sprintf("{0x%X, 0x%X}, ", $a, $b);
        if (strlen($line) + strlen($item) > 118) { echo rtrim($line), "\n"; $line = "\t"; }
        $line .= $item;
    }
    echo rtrim($line), "\n}\n\n";
}
// Extended_Pictographic ranges as PCRE in this PHP build sees them
$ranges = []; $start = null;
for ($cp = 0; $cp <= 0x10FFFF; $cp++) {
    if ($cp >= 0xD800 && $cp <= 0xDFFF) { $in = false; }
    else { $in = (bool) preg_match('/[\p{ExtPict}\x{1F1E6}-\x{1F1FF}#*0-9]/u', mb_chr($cp, 'UTF-8')); }
    if ($in && $start === null) $start = $cp;
    if (!$in && $start !== null) { $ranges[] = [$start, $cp - 1]; $start = null; }
}
if ($start !== null) $ranges[] = [$start, 0x10FFFF];
echo "// Code generated from symfony/string's wcswidth tables and PCRE's Extended_Pictographic property; DO NOT EDIT.\n\npackage console\n\n";
emit('wcwidthWide', $wide);
emit('wcwidthZero', $zero);
emit('emojiChars', $ranges);
