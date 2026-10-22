<?php
// Maps JSON payloads with upstream Shlink's own TreeMapper and prints the result, so the Go port's request mapping can
// be checked against it. Input (stdin): a JSON list of {"class": "...", "payload": {...}}. Output: a JSON list of
// {"ok": {...public props...}} or {"invalidElements": [...], "messages": {...}}.
// Run: docker run --rm -i --entrypoint php -v $PWD/tools/oracle:/oracle shlinkio/shlink:5.1.8 /oracle/map.php < cases.json

declare(strict_types=1);

chdir('/etc/shlink');
putenv('APP_ENV=prod');
/** @var Psr\Container\ContainerInterface $container */
$container = require 'config/container.php';
$mapper = $container->get(CuyZ\Valinor\Mapper\TreeMapper::class);

function export(mixed $value): mixed
{
    if ($value instanceof DateTimeInterface) {
        return $value->format(DateTimeInterface::ATOM);
    }
    if ($value instanceof BackedEnum) {
        return $value->value;
    }
    if ($value instanceof UnitEnum) {
        return $value->name;
    }
    if ($value instanceof Shlinkio\Shlink\Rest\Entity\ApiKey) {
        return '@apiKey';
    }
    if (is_object($value)) {
        $out = ['@class' => $value::class];
        foreach ((new ReflectionObject($value))->getProperties() as $prop) {
            $out[$prop->getName()] = export($prop->getValue($value));
        }
        return $out;
    }
    if (is_array($value)) {
        return array_map(export(...), $value);
    }
    return $value;
}

$cases = json_decode(stream_get_contents(STDIN), true, flags: JSON_THROW_ON_ERROR);
$results = [];
foreach ($cases as $case) {
    $payload = $case['payload'];
    // "@apiKey" stands for an admin API key object, which is what the REST actions pass
    if (($payload['apiKey'] ?? null) === '@apiKey') {
        $payload['apiKey'] = Shlinkio\Shlink\Rest\Entity\ApiKey::create();
    }
    try {
        $results[] = ['ok' => export($mapper->map($case['class'], $payload))];
    } catch (CuyZ\Valinor\Mapper\MappingError $e) {
        $messages = [];
        foreach ($e->messages()->errors() as $error) {
            $messages[$error->name()] = $error->toString();
        }
        $results[] = ['invalidElements' => array_keys($messages), 'messages' => $messages];
    } catch (Throwable $e) {
        $results[] = ['exception' => $e::class, 'message' => $e->getMessage()];
    }
}
echo json_encode($results, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE), "\n";
