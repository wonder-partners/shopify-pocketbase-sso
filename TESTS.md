# Validation du parcours OAuth natif par redirection

Environnement : Windows amd64, Go 1.27.2, PocketBase 0.40.5, SDK JavaScript officiel 0.28.1.

```powershell
npm ci --prefix integration --ignore-scripts --no-audit --no-fund
$env:RUN_JS_SDK_TEST = '1'
go test ./... -count=1 -cover
go vet ./...
go build ./...
node --check integration/oauth.mjs
node --check integration/sdk.mjs
git diff --check
```

Résultat du 16 octobre 2026 : succès des 49 groupes de tests du cœur, dont les deux intégrations SDK. Couverture du cœur : 86,8 %. Les deux groupes de l'hôte et les cinq groupes des migrations historiques passent aussi. `go vet`, la compilation et les vérifications JavaScript passent.

## Frontend statique de l'hôte

`frontend_test.go` vérifie la page racine, le callback `/auth/callback`, les scripts et le SDK local servis par `apis.Static`. Les en-têtes de sécurité et l'absence de cookies sont contrôlés, les paramètres du callback ne sont pas reflétés dans le HTML et les routes natives PocketBase restent prioritaires. Un deuxième test vérifie que les fonctions OAuth de `pb_public/app.js` sont identiques à celles du README déjà exécutées avec le SDK officiel.

Un contrôle Node a aussi exécuté le véritable `app.js` avec le SDK officiel local, un DOM simulé et des réponses HTTP de test. Il couvre les boutons, le callback invalide, `login_required`, la mise à jour de l'auth store, le refresh, la lecture du compte, la déconnexion locale et un mauvais chemin de callback. Cela ne remplace pas un navigateur réel.

## Nouveau contrat testé

Le frontend appelle les API natives uniquement :

1. `GET /api/collections/users/auth-methods` via `listAuthMethods()`.
2. Redirection de premier niveau vers Shopify, avec le state et PKCE natifs.
3. Vérification du state et de l'expiration dans le sessionStorage de l'onglet.
4. `POST /api/collections/users/auth-with-oauth2` via `authWithOAuth2Code()`.

Il n'existe aucun endpoint personnalisé de préparation ou de callback, aucun cookie OAuth, ni aucune transaction en mémoire côté Go. Le test vérifie que `/api/shopify/auth/start` et `/api/shopify/auth/complete` répondent HTTP 404.

Le state n'est plus validé côté serveur. Les helpers de la fixture Go émulent ce contrôle côté frontend et ne constituent pas des routes HTTP. La validation effective du JavaScript public est aussi testée avec le SDK officiel, comme décrit ci-dessous.

## Contrôles de sécurité

- Session Shopify existante, visiteur anonyme avec `prompt=none`, puis connexion interactive.
- State natif et challenge PKCE neufs à chaque appel, verifier absent de l'URL d'autorisation.
- Format du verifier obligatoire, S256, mauvais verifier, code expiré et code à usage unique.
- Nonce égal au challenge PKCE et vérifié côté serveur depuis le verifier ; substitution du nonce avec un PKCE pourtant valide refusée.
- Nonce absent, signature, algorithme, audience, issuer, expiration, `nbf` et mauvais `at_hash` refusés.
- Sujets Shopify numériques conservés exactement, sans arrondi et sans contournement de signature.
- Callback et client fixes ; POST natif refusé sans l'origine attendue ; réponses OAuth et méthodes d'auth non mises en cache.
- Aucun cookie PocketBase requis ou créé, ni accès/refresh token Shopify exposé.
- Anciennes pages, SDK embarqué, session bridge, logout et routes personnalisées absents.
- Un ancien cookie contenant un token PocketBase n'authentifie pas l'API ; l'en-tête natif reste nécessaire.
- Hooks OAuth, création, auth et enrichissement ; refus avant ou après `e.Next()` sans fuite de JWT.
- Règles natives, relations de propriétaire, MFA, renouvellement et filtrage realtime comparés aux comptes avec mot de passe.
- Comptes et liaisons OAuth conservés ; aucune fusion par e-mail ou par en-tête Authorization.
- Schéma et règles de `users` laissés au projet hôte, même pour d'anciens noms de champs du prototype.
- `_shopifyIdentities` système, règles API verrouillées, identifiants bruts non exposés, suppression par cascade depuis `users`.

Le fournisseur simulé utilise de vrais codes, PKCE, JWT signés, JWKS et GraphQL dans une instance HTTP locale. Les données de chaque test sont dans une base SQLite temporaire. Ce fournisseur n'est pas importé par le programme de production.

## Exemple frontend exécuté avec le SDK officiel

`integration/oauth.mjs` extrait et exécute directement les deux fonctions du [README](README.md). Il n'utilise pas une seconde implémentation du parcours.

Ce test réalise toute la séquence, avec le SDK officiel et de vraies requêtes aux serveurs locaux :

- Méthodes d'auth natives, tentative silencieuse anonyme, réception de `login_required` sans échange ni boucle.
- Connexion interactive au fournisseur simulé et récupération du callback.
- State incorrect, paramètres expirés, stockage absent ou corrompu et code manquant : aucun appel d'échange n'est envoyé.
- URL du callback nettoyée avant l'échange, suppression des paramètres temporaires, mise à jour automatique de `pb.authStore` et notification `onChange`.
- Rejeu du callback bloqué localement, puis tentative d'échange direct du même code refusée par le fournisseur.
- Persistance via le LocalAuthStore par défaut, nouvelle instance SDK, `authRefresh`, lecture du compte et déconnexion locale.

Le test intercepte les requêtes du SDK pour refuser toute API autre que les endpoints natifs de collections et tout cookie PocketBase. Les cookies de connexion de la boutique simulée sont séparés et ne sont envoyés qu'à cette boutique.

Node reçoit une simulation du contrat Web Storage et de la navigation. Ce n'est pas un test dans un navigateur réel ni contre Shopify. Seuls les en-têtes que le navigateur fournit automatiquement, dont Origin, sont explicitement ajoutés par le harnais Node.

`integration/sdk.mjs` vérifie aussi les règles CRUD et le renouvellement avec un compte Shopify et un compte natif. Les deux tests SDK nécessitent `RUN_JS_SDK_TEST=1`.

## Backend local et données existantes

Le binaire `bin/pocketbase-browser-test.exe` sert désormais le backend et `pb_public` en local sur `127.0.0.1:8090`. Les contrôles HTTP confirment :

- Santé de l'API et disponibilité de `users/auth-methods`.
- Nonce et challenge S256 cohérents, paramètres natifs présents, réponse `no-store`, aucun cookie créé.
- Anciens endpoints personnalisés en HTTP 404 et origine étrangère refusée en HTTP 403.

La sauvegarde SQLite `pb_backups/before-native-redirect-20261015.db` précède le redémarrage. Une comparaison intégrale après redémarrage confirme que les lignes de `users`, `_shopifyIdentities` et `_externalAuths` sont inchangées. `PRAGMA quick_check` retourne `ok`.

La sauvegarde supplémentaire `pb_backups/before-public-frontend-20261016.db` précède le redémarrage avec les fichiers publics. Les comptes et liaisons ont de nouveau été comparés intégralement et sont inchangés ; le contrôle SQLite est valide. L'asset SDK servi est identique à celui du paquet officiel local.

L'app Shopify, sa configuration et le callback enregistré n'ont pas été modifiés. Le tunnel de l'ancienne démonstration reste arrêté. Le nouveau frontend de test n'est pas encore exposé en HTTPS et le parcours n'a pas été retesté contre la vraie boutique. La vérification visuelle par les outils de navigateur n'a pas été possible dans cet environnement ; les contrôles ci-dessus sont HTTP et automatisés.

Les migrations historiques restent testées séparément et ne sont pas importées par `main.go` ou le cœur. Les validations antérieures contre Shopify restent dans [docs/prototype-tests.md](docs/prototype-tests.md).
