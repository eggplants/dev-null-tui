<?php

declare(strict_types=1);

// Scramble config copied into a checkout of dev-null.sh to export the OpenAPI documents published on GitHub Pages.
// SCRAMBLE_API picks the API: v1 (the mobile app's, which dn uses) or cli (the `devnull commit` script's).

use App\Http\Middleware\AuthenticateSignedRequest;
use App\Http\Middleware\AuthenticateSshSignature;
use Dedoc\Scramble\SecurityDocumentation\MiddlewareAuthSecurityStrategy;
use Dedoc\Scramble\Support\Generator\SecurityScheme;

$api = env('SCRAMBLE_API', 'v1');

$scheme = SecurityScheme::apiKey('header', 'X-SSH-Signature')
    ->as('sshsig')
    ->setDescription(match ($api) {
        'cli' => 'An armored SSHSIG of the request body, signed with an SSH key on the author\'s GitHub account.',
        default => "An armored SSHSIG (namespace `devnull-api`) of `<unix timestamp>\\n<nonce>\\n<METHOD>\\n<path and query>\\n<body>`, "
            .'signed with an SSH key the user approved as a device. The timestamp and nonce are also sent in the '
            .'`X-Devnull-Timestamp` and `X-Devnull-Nonce` headers.',
    });

return [
    'api_path' => "api/{$api}",
    'api_domain' => null,
    'export_path' => "{$api}.json",
    'openapi_version' => \Dedoc\Scramble\OpenApiVersion::V3_1,
    'info' => [
        'version' => env('API_VERSION', '0.0.1'),
        'description' => match ($api) {
            'cli' => 'The API the `devnull commit` script posts signed commits to.',
            default => 'The API the /dev/null mobile app and [dn](https://github.com/eggplants/dev-null-tui) use.',
        }.(env('SOURCE_COMMIT') === null ? '' : sprintf(
            "\n\nGenerated at %s from [nunomaduro/dev-null.sh@%s](https://github.com/nunomaduro/dev-null.sh/tree/%2\$s).",
            env('GENERATED_AT'),
            env('SOURCE_COMMIT'),
        )),
    ],
    'ui' => ['title' => "/dev/null API ({$api})"],
    'servers' => ['dev-null.sh' => "https://dev-null.sh/api/{$api}"],
    'enum_cases_description_strategy' => 'description',
    'enum_cases_names_strategy' => false,
    'flatten_deep_query_parameters' => true,
    'middleware' => [],
    'extensions' => [],
    'security_strategy' => [
        MiddlewareAuthSecurityStrategy::class,
        [
            'middleware' => [AuthenticateSignedRequest::class, AuthenticateSshSignature::class],
            'scheme' => $scheme,
        ],
    ],
];
