<?php
require '/etc/shlink/vendor/autoload.php';
use Symfony\Component\Console\Helper\ProgressBar;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\BufferedOutput;
use Symfony\Component\Console\Output\OutputInterface;
use Symfony\Component\Console\Style\SymfonyStyle;
$cases = [
  // name => [decorated, verbosity, calls]
  'jump-to-max' => [false, OutputInterface::VERBOSITY_NORMAL, [['max', 1000], ['progress', 1000], ['finish']]],
  'rapid-steps' => [false, OutputInterface::VERBOSITY_NORMAL, [['max', 300], ['progress', 0], ['progress', 100], ['progress', 200], ['progress', 300], ['finish']]],
  'first-then-max' => [false, OutputInterface::VERBOSITY_NORMAL, [['max', 52428800], ['progress', 16384], ['progress', 52428800], ['finish']]],
  'unknown-size' => [false, OutputInterface::VERBOSITY_NORMAL, [['max', 0], ['progress', 0], ['progress', 4096], ['finish']]],
  'over-max' => [false, OutputInterface::VERBOSITY_NORMAL, [['max', 10], ['progress', 15], ['finish']]],
  'decorated' => [true, OutputInterface::VERBOSITY_NORMAL, [['max', 300], ['progress', 0], ['progress', 300], ['finish']]],
  'verbose' => [false, OutputInterface::VERBOSITY_VERBOSE, [['max', 300], ['progress', 300], ['finish']]],
  'very-verbose' => [false, OutputInterface::VERBOSITY_VERY_VERBOSE, [['max', 300], ['progress', 300], ['finish']]],
  'quiet' => [false, OutputInterface::VERBOSITY_QUIET, [['max', 300], ['progress', 300], ['finish']]],
];
$out = [];
foreach ($cases as $name => [$decorated, $verbosity, $calls]) {
  $buffer = new BufferedOutput($verbosity, $decorated);
  $io = new SymfonyStyle(new ArrayInput([]), $buffer);
  $io->text('<fg=blue>Downloading GeoLite2 db file...</>');
  $bar = new ProgressBar($io);
  foreach ($calls as $c) {
    match ($c[0]) {
      'max' => $bar->setMaxSteps($c[1]),
      'progress' => $bar->setProgress($c[1]),
      'finish' => $bar->finish(),
    };
  }
  $io->success('GeoLite2 db file properly downloaded.');
  $out[$name] = $buffer->fetch();
}
echo json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES);
