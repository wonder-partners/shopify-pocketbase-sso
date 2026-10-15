# Copier dans config.local.ps1, puis executer : . ./config.local.ps1
# Le fichier local est ignore par Git. Ne jamais y mettre un token Admin API.
$env:SHOPIFY_SHOP_URL = 'https://ta-boutique.myshopify.com'
$env:STUDIO_URL = 'https://ton-url-https-de-test.example'
$env:SHOPIFY_CUSTOMER_CLIENT_ID = 'client-id-de-notre-app-ou-du-client-Headless'
# Notre app avec customer_authentication, ou client Headless public : PKCE, secret vide.
$env:SHOPIFY_CUSTOMER_CLIENT_SECRET = ''
# Client Headless confidential uniquement : saisir le secret sans echo ni historique.
# $env:SHOPIFY_CUSTOMER_CLIENT_SECRET = [System.Net.NetworkCredential]::new('', (Read-Host 'Client secret Customer Account API' -AsSecureString)).Password
$env:SSO_ALLOW_LOOPBACK_HTTP = '0'
