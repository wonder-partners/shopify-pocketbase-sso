# Validation du parcours OAuth natif par redirection

Environnement : Windows amd64, Go 1.27.2, PocketBase 0.40.5, SDK JavaScript officiel 0.28.1.

```powershell
mise install
mise run check
```

`mise run check` prépare les dépendances, fixe `RUN_JS_SDK_TEST=1` pour la suite et lance les contrôles Go/JavaScript ainsi que `git diff --check`. Pour lancer seulement les tests : `mise run test`. Les trois intégrations SDK sont incluses par défaut. La configuration locale Shopify n'est pas nécessaire pour ces fixtures.

Validation du passage à mise le 16 octobre 2026 : `mise tasks validate`, installation des outils fixés et `mise run check` réussis sur Windows. Go `1.27.2`, Node `24.14.1`, npm `11.11.0`. Les cinq variables locales migrées ont été comparées en mémoire au résultat de `mise exec` et sont identiques, sans afficher leurs valeurs. Aucun backend n'a été lancé sur la base réelle.

Après nettoyage du mode confidential le même jour, `mise run check` passe à nouveau avec les trois intégrations SDK. Le code et les fixtures utilisent uniquement le client public PKCE. Le simulateur refuse tout paramètre `client_secret`, même vide, et tout en-tête `Authorization` au token endpoint. Les tests vérifient donc aussi que le marqueur interne exigé par PocketBase n'est pas transmis. Couvertures après retrait : hôte 48,4 %, cœur 86,5 %, migrations 81,0 %. Aucune configuration locale privée n'est nécessaire.

cloudflared `2026.10.1` est désormais installé par mise hors du dépôt. Version, chemin et égalité SHA-256 avec l'ancienne copie vérifiés avant retrait de `bin/cloudflared.exe`. Les neuf tâches sont valides et la commande de `tunnel` a été vérifiée en dry-run. Aucun tunnel n'a été ouvert pendant ce contrôle.

Passage ultérieur au tunnel nommé `studio-sso-dev` sur `sso.example.com` : onze tâches valides, validation ingress et contrôle des règles par `cloudflared tunnel --config cloudflared.yml ingress validate` et `ingress rule`. Quatre connexions QUIC établies pendant un démarrage temporaire. Les contrôles HTTPS réels de l'admin `/_/` et de l'endpoint superuser renvoient 404 conformément à l'allowlist ingress. Le backend sur base réelle n'a pas été démarré ; le callback Shopify externe reste à enregistrer. Ces contrôles ne démontrent pas encore le login Shopify réel.

Callback enregistré ensuite le 16 octobre 2026 : configuration de `Studio SSO` récupérée via Shopify CLI, remplacement du seul `customer_authentication.redirect_uris` par `https://sso.example.com/auth/callback`, validation CLI JSON `{ "valid": true, "issues": [] }`, puis publication de `studio-sso-4`. Une seconde récupération depuis Shopify dans un dossier temporaire confirme le callback publié. Aucun test de connexion client réel n'a été réalisé à cette étape.

Premier essai utilisateur sur le domaine fixe : erreur `redirect_uri mismatch`, reproduite dans un profil navigateur privé malgré une requête correcte et `studio-sso-4` active. Après `shopify app dev clean` ciblé sur `Studio SSO` et `example-store.myshopify.com`, la CLI confirme la restauration de la version active. Nouvelle vérification navigateur contre les vrais serveurs Shopify : tentative silencieuse sans session, retour au studio proposant le bouton, puis connexion interactive ouvrant le formulaire réel de la boutique. Aucun e-mail/code soumis, aucune authentification complète ni lecture API après login réel encore validée. Aucun changement de code nécessaire pour cette correction.

Résultat du 15 octobre 2026 : succès des 49 groupes de tests du cœur, dont les deux intégrations SDK. Couverture du cœur : 86,8 %. Les cinq groupes des migrations historiques passent aussi, avec une couverture de 81,0 %. `go vet`, la compilation et les vérifications JavaScript passent.

Résultat du 16 octobre 2026 après ajout du frontend inline : suite complète réussie avec `RUN_JS_SDK_TEST=1`, dont les trois tests racine et les trois intégrations SDK au total. Couvertures : hôte 48,4 %, cœur 86,8 %, migrations 81,0 %. `go vet`, `go build`, syntaxe des trois scripts Node et `git diff --check` passent. Le module inline HTML est compilé et exécuté par le test frontend, puis vérifié dans le navigateur avec le SDK CDN. Les changements sont locaux, non commités, sur la base de HEAD `75d85a7`.

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

## Frontend hôte ajouté le 16 octobre 2026

Trois tests racine couvrent le service statique et la page :

- `TestFrontendRoutes` vérifie la page HTML unique et le callback, les en-têtes CSP/no-store/no-referrer et les empreintes des blocs inline, l'absence de cookies ou de réflexion des paramètres OAuth, les 404 des anciennes routes et des fichiers absents, et la conservation des routes API/admin natives.
- `TestFrontendMatchesREADMEAndPinnedSDK` vérifie que les deux fonctions OAuth de `pb_public/index.html` sont identiques au README et que l'import CDN utilise la version fixée dans `integration/package.json`, actuellement `0.28.1`.
- `TestFrontendWithOfficialSDK`, activé avec `RUN_JS_SDK_TEST=1`, exécute tout le module inline de `index.html` via `integration/frontend.mjs`, avec le vrai SDK et les serveurs HTTP simulés. Il vérifie les parcours silencieux et interactif, la consommation du stockage et le nettoyage de l'adresse avant échange, le refresh/lecture native, la persistance, le logout local sans boucle, la reconnexion silencieuse, le rejeu et le mauvais state, le rendu sûr des erreurs, le signalement MFA et les conflits sans fusion. Le DOM, la navigation et Web Storage sont simulés dans ce test Node. La MFA et le conflit sont injectés pour vérifier leur affichage ; les tests du cœur vérifient leur application côté serveur.

Une vérification dans le navigateur collaboratif T3 a aussi utilisé des serveurs de fixture et une base temporaire. Le SDK a effectivement été chargé depuis l'URL CDN fixée, avec la CSP finale. L'échange OAuth simulé, `authRefresh` et la lecture du compte ont réussi. Rechargement avec auth store restauré, logout local et reconnexion silencieuse après logout ont été vérifiés. Le retour silencieux anonyme avec proposition du bouton a aussi été observé avant le passage au CDN. Le fournisseur simulé n'a pas de formulaire de login : sa session de test a été ouverte par un POST sur son endpoint `/login`, puis la navigation OAuth a repris. Aucun code e-mail réel n'est intervenu.

Cette vérification n'est pas un test contre Shopify, Safari ou un appareil mobile. Les serveurs temporaires ont été arrêtés et le helper temporaire retiré. Aucune base réelle ni configuration Shopify n'a été modifiée. Le frontend utilise `/auth/callback` ; l'alignement des configurations encore sur `/studio/callback` et le test HTTPS réel restent à faire.

## Backend local et données existantes, historique du 15 octobre

Le nouveau binaire `bin/pocketbase-native-redirect.exe` a remplacé le backend local sur `127.0.0.1:8090`. Les contrôles HTTP confirment :

- Santé de l'API et disponibilité de `users/auth-methods`.
- Nonce et challenge S256 cohérents, paramètres natifs présents, réponse `no-store`, aucun cookie créé.
- Anciens endpoints personnalisés en HTTP 404 et origine étrangère refusée en HTTP 403.

La sauvegarde SQLite `pb_backups/before-native-redirect-20261015.db` précède le redémarrage. Une comparaison intégrale après redémarrage confirme que les lignes de `users`, `_shopifyIdentities` et `_externalAuths` sont inchangées. `PRAGMA quick_check` retourne `ok`.

L'app Shopify, sa configuration et le callback enregistré n'ont pas été modifiés. Le tunnel de l'ancienne démonstration reste arrêté ; aucun frontend de démo n'a été réintroduit. Le nouveau parcours n'a pas encore été retesté contre la vraie boutique depuis un frontend hôte.

Les migrations historiques restent testées séparément et ne sont pas importées par `main.go` ou le cœur. Les validations antérieures contre Shopify restent dans [docs/prototype-tests.md](docs/prototype-tests.md).
