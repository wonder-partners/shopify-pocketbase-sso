# Historique des validations du prototype

Résultats conservés comme historique. La démonstration HTML, les cookies de session personnalisés et la gateway ont été retirés depuis. Les contrôles actuels sont décrits dans TESTS.md à la racine.

Exécution le 15 octobre 2026, Windows amd64, Go 1.27.2 et PocketBase 0.40.5.

Le prototype passe les tests locaux. L'app est installée sur une vraie dev store Basic non-Plus. L'utilisateur a confirmé sa connexion Shopify, puis l'accès à son compte `users` par l'API PocketBase avec le SDK officiel. Il a ensuite confirmé le nouveau parcours `authWithOAuth2Code`. La section « Fournisseur OAuth2 natif » décrit l'état actuel ; les étapes précédentes sont conservées comme historique.

## Tests automatisés

```powershell
npm ci --prefix integration --ignore-scripts --no-audit --no-fund
$env:RUN_JS_SDK_TEST = '1'
go test ./... -count=1 -cover
go vet ./...
node --check internal/studio/web/client.js
node --check internal/studio/web/callback.js
node --check integration/sdk.mjs
node --check integration/oauth.mjs
```

Résultat : succès, 46 groupes de tests dans `internal/studio`, avec une couverture d'instructions de 85,8 %, et 5 groupes de tests des migrations avec une couverture de 81,0 %. La gateway atteint 65,2 %. `go vet`, les vérifications de syntaxe JavaScript et les compilations passent aussi.

Les tests Node exécutent réellement le SDK officiel PocketBase `0.28.1` contre une instance HTTP de test, y compris `authWithOAuth2Code`. Sans `RUN_JS_SDK_TEST=1`, ces tests sont ignorés ; les autres tests Go restent exécutés.

| Scénario | Résultat |
| --- | --- |
| Session boutique existante, client public avec PKCE | Passe |
| Session boutique existante, ancien client confidential avec HTTP Basic | Test historique, mode supprimé le 16 octobre 2026 |
| Visiteur anonyme, lien de connexion, puis parcours interactif | Passe |
| State invalide, cookie absent, autre navigateur et rejeu du callback | Rejetés |
| Mauvais nonce, audience ou émetteur du jeton d'identité | Rejetés |
| Sujet Shopify numérique, y compris au-delà de la précision d'un float64 | Session créée, identifiant conservé exactement |
| Sujet numérique avec fausse signature, HS256 non autorisé, mauvais émetteur/audience/nonce, expiration ou `nbf` futur | Rejetés, aucune session créée |
| Normalisation de `sub` négatif, fractionnaire, en notation exponentielle ou de type inattendu | Rejetée |
| Jeton expiré ou signature créée avec une autre clé | Rejetés |
| Altération du challenge PKCE | Rejetée |
| Erreurs GraphQL de permissions | Aucune session créée |
| Changement d'e-mail d'une identité existante | Même compte PocketBase |
| Autre identité portant le même e-mail | Conflit, aucune fusion de compte |
| E-mail du profil différent du claim vérifié | Pas de marquage `verified` |
| Modification publique des champs d'identité Shopify | Bloquée ; l'inscription native reste soumise à `CreateRule` |
| Appel de l'API PocketBase avec le jeton émis | Autorisé pour son propre profil |
| Renouvellement via `auth-refresh` | Réponse native valide, jeton renouvelable selon la politique de collection |
| Compte Shopify et compte créé avec mot de passe natif | Mêmes règles de liste, lecture, création, modification et suppression des documents privés |
| Accès aux données d'un autre compte ou changement de propriétaire | Refusés pour les deux méthodes d'authentification |
| Middleware `RequireAuth` et `e.Auth` | Compte chargé normalement pour les deux méthodes, visiteur refusé |
| `AuthRule` et refus dans `OnRecordAuthRequest`, avant ou après `e.Next` | Refus respectés, aucun jeton ou cookie de succès exposé |
| MFA activée pour le compte Shopify | Challenge natif requis, puis second facteur natif accepté |
| Inscription Shopify avec `CreateRule` verrouillée | Refusée, aucun compte créé |
| Politique existante de la collection | Règles, durée des jetons et réglages d'authentification conservés |
| Hooks d'enrichissement et personnalisation de la réponse d'auth | Champs, métadonnées et jeton retournés par PocketBase conservés |
| SDK officiel, compte Shopify et compte natif | Auth store, refresh, lecture et CRUD soumis aux mêmes règles |
| Realtime local, compte Shopify et compte natif | Événement du propriétaire reçu, événement d'un autre compte filtré |
| Gateway publique | Collections autorisées accessibles, admin et paramètres bloqués, chemins non canoniques refusés |
| Jeton PocketBase signé mais expiré | Rejeté |
| Requête de session ou de déconnexion depuis une autre origine | Bloquée |
| Déconnexion locale | Cookie supprimé, session locale absente |
| Transaction de connexion expirée | Rejetée |
| Configuration HTTP en dehors du loopback explicitement autorisé | Rejetée |
| Cookie en configuration HTTPS | Secure, HttpOnly, SameSite=Lax, préfixe `__Host-`, sans Domain |
| Fournisseur OIDC natif de PocketBase | Extraction de l'identité et validation JWKS vérifiées |
| Fournisseur Go `shopify` et `authWithOAuth2Code` du SDK officiel | Inscription et connexion par l'endpoint OAuth natif, auth store alimenté automatiquement |
| Hooks OAuth2 et création de compte | Déclenchés avec le contexte OAuth2 ; leurs refus sont respectés avant et après `e.Next` |
| Liaison `_externalAuths` et reconnexion | Liaison native créée une seule fois, même compte retrouvé |
| Ancien compte Shopify déjà dans `users` | Identifiant et relation métier conservés, liaison native ajoutée au premier login |
| Code non préparé, autre navigateur, mauvaise origine ou callback différent | Refusés par l'endpoint OAuth natif |
| Code préparé expiré, mauvais verifier PKCE et rejeu | Refusés |
| Compte existant dans l'en-tête Authorization | Ne devient pas une cible de liaison implicite |
| Profil falsifié dans `createData` | Identité et e-mail imposés par Shopify ; impossible de forcer `verified` ou un mot de passe choisi par le client |
| Credentials Shopify dans les métadonnées OAuth | Access token, refresh token et ID token non exposés |
| Autres fournisseurs et politiques d'auth | Conservés lors de la configuration Shopify |
| Migration vers `_shopifyIdentities`, puis second démarrage | Identifiants bruts, utilisateur, relation métier et liaison OAuth native conservés ; aucun doublon |
| Ancienne identité incomplète, schéma partiel, identité en conflit ou règle dépendant d'un ancien champ | Migration refusée, anciennes données et règles conservées, copie privée annulée |
| API privée par nom et identifiant de collection, avec et sans jeton utilisateur | Liste, lecture, création, modification et suppression refusées |
| Expansion inverse depuis `users`, réponses OAuth et page HTML | Aucun identifiant Shopify brut exposé |
| Abonnement realtime à la collection privée et à un compte précis | Aucun événement privé reçu, événements métier autorisés toujours reçus |
| Ligne privée absente mais liaison OAuth native exacte présente | Ligne privée reconstruite, même utilisateur |
| Ligne privée et liaison native désignant deux utilisateurs différents | Connexion refusée |
| Suppression d'un utilisateur | Identité Shopify privée supprimée par cascade |
| Profil Shopify limité à l'identifiant et à l'e-mail | Connexion acceptée, aucun champ de nom ajouté ni synchronisé par le package d'auth |
| Champs de nom propres à un projet hôte | Conservés par le package lors du démarrage et des reconnexions Shopify |
| Promotion d'une collection d'identités ordinaire en système | Même collection et même enregistrement après deux démarrages, même compte à la connexion OAuth |
| Schéma système | Suppression, renommage et ouverture des règles refusés par PocketBase |
| Migration ponctuelle de suppression des noms | Compte, session, mot de passe, champs personnalisés et règles conservés ; nouveau champ créé ensuite non supprimé au redémarrage |
| Nettoyage des noms avec règle dépendante ou type incompatible | Migration refusée sans suppression partielle des champs |
| Nettoyage sur installation neuve | Aucune collection ni aucun champ créés |
| Ancienne variable `STUDIO_AUTH_COLLECTION=members` | Ignorée par le flux de connexion ; le callback cible `users` |
| Tentative OAuth sur une autre collection ou `_superusers`, par nom et identifiant | HTTP 403 ; le même code reste utilisable uniquement sur `users` |
| Cookie portant un jeton valide d'une autre collection d'auth | Refusé par la session du studio |
| Renommage `_shopifyAccounts` vers `_shopifyIdentities`, et opération inverse | Même collection, enregistrements, identifiants exacts, relations et index d'unicité |
| Renommage avec les deux noms présents, une source publique ou du mauvais type | Refus sans suppression ni fusion |

Les tests utilisent des serveurs HTTP réels sur des ports locaux aléatoires, une clé RSA et son JWKS, des codes d'autorisation à usage unique, un cookie de boutique simulée et une base SQLite PocketBase distincte par test. Le faux endpoint de jeton vérifie le challenge PKCE. Sa branche historique de vérification du secret confidential a été supprimée le 16 octobre 2026 ; il refuse maintenant tout paramètre `client_secret` et tout en-tête d'authentification du client. Le faux endpoint GraphQL exige l'access token brut attendu par Shopify.

Le serveur simulé est sous notre contrôle. Ces tests vérifient le comportement du client et de PocketBase face au contrat simulé ; ils ne constituent pas un test des serveurs Shopify.

## Identités Shopify et collection `users` fixe

Le 15 octobre 2026, `bin/studio-identities.exe` a remplacé `bin/studio-system.exe` sur le port `8090`, après sauvegarde SQLite cohérente dans `pb_backups/before-identities-users-only-20261015.db`. Le tunnel, la gateway et le callback Shopify sont inchangés.

La configuration active ne contient plus `Config.AuthCollection` ni la lecture de `STUDIO_AUTH_COLLECTION` : le parcours cible uniquement `users`. Les anciennes migrations appliquées restent inchangées, y compris leur configuration historique de nettoyage, qui ne choisit pas la cible de connexion.

La nouvelle migration Go native `1792097400_rename_shopify_identities.go` a renommé `_shopifyAccounts` en `_shopifyIdentities`. La comparaison locale avec la sauvegarde confirme le même identifiant de collection, les mêmes enregistrements privés avec tous leurs identifiants bruts, la relation vers `users`, et des tables `users` et `_externalAuths` strictement inchangées. L'ancienne table a disparu par renommage, le statut système et les cinq règles verrouillées sont conservés, et la migration figure dans le journal. SQLite `quick_check` retourne `ok`.

Après déploiement, le studio et les méthodes d'auth de `users` répondent HTTP 200. Les deux noms de collection privée, l'admin et les paramètres restent bloqués sur le tunnel avec HTTP 404. En accès local direct sans authentification, le nouveau nom retourne HTTP 403 et l'ancien HTTP 404.

Le parcours complet est testé avec le SDK officiel contre le serveur Shopify simulé. Aucune nouvelle connexion sur la boutique réelle n'a encore été confirmée pour cette dernière version.

## Historique de la collection système et de la suppression des noms

Le 15 octobre 2026, `bin/studio-system.exe` a remplacé `bin/studio-private.exe` sur le port `8090`. La sauvegarde SQLite cohérente `pb_backups/before-system-email-only-20261015.db` a été créée avant le changement. Le tunnel, la gateway, le client Shopify et son callback sont inchangés.

Le démarrage applique la migration Go native `1792095899_remove_profile_names.go`, qui supprime les champs `first_name` et `last_name` de la collection d'auth configurée, puis journalise son exécution. Le code d'auth ne les recrée plus et ne demande plus `firstName` ou `lastName` à Shopify. La nouvelle requête limitée à l'identifiant et à l'e-mail a été validée contre le schéma Customer Account API `2026-10`.

Une comparaison locale avec la sauvegarde confirme : les deux champs supprimés, la collection `_shopifyAccounts` passée à `system: true` sans changement d'identifiant, les comptes privés et les liaisons `_externalAuths` inchangés, et les identifiants, e-mails, mots de passe, indicateurs de vérification et clés de session des utilisateurs inchangés. Le journal de migration contient le nettoyage. Le contrôle SQLite `quick_check` retourne `ok`.

Le code de l'admin embarqué dans PocketBase `0.40.5` classe les collections portant cet attribut dans **System**, hors de la liste normale et repliées par défaut. Les collections épinglées gardent leur groupe **Pinned**. Le statut système n'enlève pas au superuser le droit de consulter leurs enregistrements. Après déploiement, le studio et ses méthodes d'auth répondent HTTP 200 ; la collection privée, l'admin et les paramètres restent bloqués sur le tunnel. PocketBase refuse toujours la lecture directe de la collection privée sans authentification avec HTTP 403.

Le parcours OAuth complet est validé par les tests HTTP et le SDK officiel contre le serveur Shopify simulé. Cette dernière version n'a pas encore fait l'objet d'une nouvelle confirmation de connexion sur la boutique réelle ; les identités et credentials locaux nécessaires ont été vérifiés comme inchangés.

## Historique du stockage privé des identités Shopify

Le 15 octobre 2026, `bin/studio-private.exe` a remplacé `bin/studio-oauth2.exe` sur le même port `8090`, sans changer le tunnel, la gateway ni le callback Shopify. La sauvegarde SQLite `pb_backups/before-private-accounts-20261015.db` a été créée avant la migration.

Une comparaison locale avec cette sauvegarde confirme : un utilisateur avant et après avec le même identifiant, une ligne `_shopifyAccounts` contenant les trois valeurs brutes exactes, les trois anciens champs retirés de `users`, et la liaison `_externalAuths` inchangée. Les deux anciens comptes de démonstration `studio_customers` sont conservés. Les cinq règles API de la nouvelle collection sont verrouillées. Le contrôle SQLite `quick_check` retourne `ok`. Aucun identifiant client, e-mail ou jeton réel n'a été affiché lors de ces contrôles.

Après déploiement, le studio, le script de callback et les méthodes d'auth de `users` répondent HTTP 200. La collection privée est bloquée par la gateway publique avec HTTP 404 et par PocketBase directement avec HTTP 403 sans authentification. L'admin et les paramètres restent bloqués sur le tunnel.

À cette étape, la requête de profil demandait encore les noms nullable et le serveur simulé vérifiait leur synchronisation. Cette fonctionnalité a depuis été retirée à la demande de l'utilisateur, comme décrit dans la section précédente.

## Première démonstration locale, avant l'alignement API

Compilation réussie de `bin/studio.exe`, `bin/mockshop.exe` et `bin/gateway.exe` avec Go 1.27.2.

Après lancement des trois exécutables, des requêtes HTTP avec un conteneur de cookies ont confirmé :

- Arrivée invitée au studio : page HTTP 200 avec le lien de connexion.
- Connexion à la boutique simulée puis ouverture du studio : profil de Camille affiché après les redirections.
- Récupération de la session locale puis lecture du profil par l'API PocketBase : HTTP 200.
- Gateway, `/studio?checked=1` : HTTP 200.
- Gateway, `/_/` et `/api/health` : HTTP 404.

Les serveurs de cette première démonstration simulée ont été arrêtés. À ce stade, aucun tunnel public n'avait été ouvert. Le test réel préparé ensuite est décrit ci-dessous.

## Contrôle des dépendances

```powershell
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -show verbose ./...
```

Le premier contrôle avec Go 1.27.0 et les dépendances réseau initiales signalait des vulnérabilités connues. Le projet a été passé à Go 1.27.2, `golang.org/x/net v0.61.0` et `golang.org/x/crypto v0.58.0`. Les tests, l'analyse statique et les compilations ont ensuite été relancés avec succès.

Résultat final de govulncheck : zéro vulnérabilité atteignable détectée et zéro dans les packages importés. Le contrôle signale encore l'avis [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) sur les packages OpenPGP du module `x/crypto`. Ces packages ne sont pas importés par le projet. Ce contrôle n'est pas un audit de sécurité complet.

Le contrôle a été relancé après l'alignement PocketBase, avec le même résultat. `npm audit --json` dans `integration` ne signale aucune vulnérabilité. Le SHA-256 du SDK servi au navigateur correspond à celui du fichier distribué par le paquet npm officiel installé.

## Fournisseur OAuth2 natif

Le 15 octobre 2026, la version `bin/studio-oauth2.exe` a remplacé le serveur précédent sur le port local `8090`, sans changer le tunnel, la gateway ni le callback Shopify. Aucun redéploiement d'app Shopify n'était nécessaire. Une sauvegarde SQLite cohérente a été créée au préalable dans `pb_backups/before-native-oauth2-20261015.db`.

Le navigateur appelle désormais `authWithOAuth2Code('shopify', …)` après contrôle du callback par le serveur. Le fournisseur échange le vrai code Shopify dans le traitement natif `auth-with-oauth2`. La création du compte et de la liaison `_externalAuths` appartient à PocketBase, avec ses hooks OAuth2 et de création. Le SDK officiel a été testé contre cet endpoint avec un fournisseur Shopify simulé, un sujet numérique, des champs `createData` falsifiés et un rejeu du code. Les tests de règles d'accès, MFA et realtime précédents passent aussi par ce nouveau parcours.

Contrôles distants après déploiement :

| Contrôle | Résultat |
| --- | --- |
| `/studio?checked=1`, `/studio/callback.js` et SDK local | HTTP 200 |
| `/api/collections/users/auth-methods` | Fournisseur `shopify` présent, aucun secret de configuration exposé |
| Appel OAuth depuis une origine étrangère | HTTP 403 |
| Admin `/_/` et paramètres `/api/settings` | HTTP 404 |

L'utilisateur a ouvert explicitement `/studio/auth` et confirmé son profil avec « API PocketBase : accès confirmé avec le SDK officiel. ». Ce test force un nouveau parcours Shopify, au lieu de réutiliser seulement le cookie PocketBase déjà présent.

Une lecture locale en comparaison avec la sauvegarde a confirmé un compte Shopify avant et après, le même identifiant, et une liaison native `shopify` dont le `providerId` correspond au hash émetteur/sujet attendu. Aucune donnée personnelle ou valeur de token n'a été affichée.

L'ancienne limite concernant l'absence de hooks OAuth2 et de création est donc levée. Restent distincts : l'extraction en module Go installable, l'interface MFA, l'envoi réel des alertes et la synchronisation des déconnexions Shopify/PocketBase. Les sessions restent indépendantes.

## Historique de l'alignement PocketBase et API

Cette étape a été déployée sur le même tunnel le 15 octobre 2026 avec `bin/studio-native.exe` et `bin/gateway-native.exe`. Les compilations, les tests complets avec le SDK JavaScript et `go vet` passent.

Le compte Shopify utilise désormais `users`, un jeton renouvelable standard et la finalisation `apis.RecordAuthResponse`. Les tests comparatifs créent aussi un compte par l'endpoint natif d'inscription, le connectent avec `auth-with-password` et vérifient les mêmes opérations sur une collection de documents avec relation de propriétaire. Ils testent les hooks et règles dans PocketBase, sans les remplacer par un faux moteur de permissions.

Le test realtime ouvre une vraie connexion SSE locale, authentifie l'abonnement et compare les événements autorisés pour chaque compte. Il ne valide pas le realtime sur l'URL publique actuelle : les Quick Tunnels Cloudflare ne prennent pas en charge SSE. [Limites officielles](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/).

Contrôles distants après déploiement :

| Endpoint | Résultat |
| --- | --- |
| `/studio?checked=1` | HTTP 200 |
| `/studio/client.js` et `/studio/pocketbase.es.mjs` | HTTP 200, JavaScript servi localement |
| `/api/collections/users/records`, sans authentification | HTTP 200, résultat filtré par les règles natives |
| `/api/settings`, `/_/` et `/api/collections/_superusers/auth-with-password` | HTTP 404 |

Dans son navigateur connecté à la vraie boutique, l'utilisateur a confirmé le message « API PocketBase : accès confirmé avec le SDK officiel. ». Il apparaît uniquement après récupération de la session, enregistrement du jeton dans `pb.authStore`, puis lecture réussie du propre compte par `getOne` sur l'API native. Ce n'est pas seulement un indicateur affiché après le callback Shopify.

Les deux anciens comptes de démonstration dans `studio_customers` ont été conservés. Un compte lié à la vraie boutique est maintenant présent dans `users`. La sauvegarde cohérente précédant l'évolution du schéma est `pb_backups/before-native-auth-20261015.db`, ignorée par Git. Ces contrôles n'affichent ni données personnelles ni jetons.

À cette étape intermédiaire, le parcours Shopify restait distinct de l'endpoint OAuth2 natif. La section précédente décrit le remplacement par `authWithOAuth2Code`. Le moteur MFA était déjà testé, mais pas son interface de saisie ni l'envoi réel des alertes. Les détails d'intégration actuels sont dans le [guide](prototype-history.md#intégrer-le-code-au-studio-existant).

## Historique de validation Shopify et vérifications restantes

Une vraie boutique de développement a ensuite été créée le 15 octobre 2026 avec Shopify CLI 4.9.4, à la demande de l'utilisateur :

- Domaine : `example-store.myshopify.com`.
- Organisation : `My Store`.
- Type retourné par `shopify store list --type dev` : `dev`.
- Forfait de fonctionnalités retourné : `basic`.
- Pays de test : France, sans données de démonstration.
- Les deux endpoints de découverte de cette nouvelle boutique répondent en HTTP 200 avec un contenu JSON.

### App installée et parcours anonyme réel

Notre app `Studio SSO` a ensuite été créée dans cette organisation avec Shopify CLI. Son Client ID public est `REPLACE_WITH_SHOPIFY_CLIENT_ID`.

- Configuration active : `shopify-app/studio-sso/shopify.app.toml`.
- Validation `shopify app config validate --json` : `valid: true`, aucune issue.
- Aucune extension d'exemple déployée : zéro extension active dans le projet configuré.
- Version publiée avec succès : `studio-sso-auth-20261015`.
- `shopify app dev --store example-store.myshopify.com --no-update` : preview prêt, scope automatiquement accordé.
- Requête Admin API en lecture seule `currentAppInstallation` : installation `gid://shopify/AppInstallation/0000000000000`, titre `Studio SSO`, seul scope `customer_read_customers`.

Un tunnel HTTPS temporaire a été ouvert vers la gateway, à l'adresse `https://tunnel.example.com`. L'exécutable officiel cloudflared 2026.10.1 a été téléchargé dans `bin/` et son SHA-256 vérifié. PocketBase utilise le client public PKCE sans secret. Les processus sont laissés actifs pour permettre le test utilisateur.

Contrôles HTTP avec un conteneur de cookies neuf, sans compte client connecté :

| Contrôle sur les vrais serveurs | Résultat |
| --- | --- |
| `/studio?checked=1` via le tunnel | HTTP 200, lien de connexion présent |
| `/_/` via le tunnel | HTTP 404, admin PocketBase non exposé |
| `/studio` puis endpoint d'autorisation Shopify avec `prompt=none` | Shopify renvoie au callback avec `error=login_required` |
| Callback anonyme | HTTP 303 vers le studio, puis HTTP 200 avec le lien de connexion |
| Lien interactif `/studio/auth` | Redirections Shopify / Shop, puis HTTP 200 sur l'endpoint d'autorisation Shopify ; aucune erreur de client ou de callback détectée |

Ces contrôles confirment l'enregistrement utilisable du client et le parcours invité. Lors de ces contrôles HTTP anonymes, aucun code e-mail n'a été saisi et aucun access token client n'a été obtenu.

### Premier essai de connexion client

L'utilisateur a ensuite suivi le parcours dans son navigateur et signalé `Invalid Shopify ID token`, HTTP 401. Dans le code, cette réponse est produite après un échange de code réussi, au moment où `go-oidc` valide le jeton d'identité. À ce stade, la cause précise n'était pas encore établie et aucun profil réel n'avait été lu.

Les métadonnées publiques annoncent RS256. Le JWKS répond en HTTP 200, y compris avec l'origine du studio, et contient une clé RSA RS256 ainsi que deux clés Ed25519. L'heure locale et l'en-tête Date de Shopify concordent à la seconde lors du contrôle. Ces vérifications ne valident pas le jeton rejeté.

Un premier diagnostic à libellés fixes a été ajouté et le serveur a été redémarré avec `bin/studio-diagnostic.exe`. L'utilisateur a obtenu `verification_failed`, catégorie encore trop générale. Les erreurs brutes du vérificateur, les jetons et les valeurs des claims n'ont pas été journalisés.

Le diagnostic a ensuite été complété pour distinguer le décodage des claims, `nbf`, les claims distribués, la configuration du vérificateur, le format des signatures et les annulations/délais de requête. Il journalise uniquement des catégories fixes et les types JSON des champs OIDC connus, jamais leurs valeurs. Cette lecture diagnostique du payload non vérifié n'intervient pas dans l'authentification. Les tests vérifient ces limites. Tous les contrôles de validation du jeton sont conservés.

### Cause identifiée et correction testée localement

La nouvelle tentative utilisateur du 15 octobre 2026 à 18:26:06 UTC a produit `claims_decode_failed`. Le diagnostic des types indique `sub: number`, avec `iss`, `aud` et `nonce` de type chaîne, `exp` et `iat` de type nombre. Dans `go-oidc v3.21.0`, la signature est vérifiée avant cette erreur de décodage. Les contrôles suivants n'avaient donc pas encore tous été exécutés.

Un [signalement du 12 octobre reconnu par Shopify](https://community.shopify.dev/t/customer-account-sign-in-fails-numeric-sub-in-id-token-payload/38544) décrit ce même problème de type. Notre observation locale porte sur cette boutique et ce jeton ; elle ne prouve pas que tous les clients Shopify sont touchés.

L'adaptateur `shopifySubjectKeySet` vérifie la signature du JWT original dans le vérificateur OIDC, puis transforme uniquement un sujet numérique entier positif en chaîne décimale exacte. Il ne désactive aucun contrôle et n'arrondit pas les identifiants. Les tests confirment la création d'une vraie session PocketBase depuis des sujets numériques simulés, la conservation d'un entier supérieur à la précision de float64 et le rejet des jetons numériques altérés ou destinés à un autre client/émetteur. Des tests ciblent aussi les formes numériques ambiguës et le refus de normaliser après échec de signature.

Les tests complets et `go vet ./...` passent. Le serveur a été lancé avec `bin/studio-fixed.exe`, compilé avec cette correction. L'URL HTTPS et le callback n'ont pas changé. À son lancement, aucun compte PocketBase issu de la vraie boutique n'était présent ; le compte de démonstration simulé reste distinct.

### Connexion réelle confirmée

Après une nouvelle ouverture du studio, l'utilisateur a confirmé voir son profil connecté. Une lecture locale limitée à des compteurs et indicateurs de présence a confirmé :

- Un compte PocketBase associé à l'émetteur de la vraie boutique `example-store`.
- Un e-mail, un identifiant client Shopify et un sujet OIDC présents.
- Un sujet conservé en texte après normalisation.
- Prénom et nom vides dans le profil retourné pour ce client de test.

Le flux connecté a donc atteint la lecture Customer Account API, l'enregistrement du compte PocketBase et l'affichage de la page authentifiée. Aucune valeur d'e-mail, d'identité client ou de jeton n'a été affichée dans les sorties de diagnostic.

L'utilisateur a signalé `[email protected]` à l'écran. La base ne contient pas ce texte et contient bien une adresse avec `@`. Cloudflare documente cette réécriture automatique du HTML et l'injection d'un script de décodage ; la politique CSP du studio bloque ces scripts injectés et autorise maintenant uniquement ses propres scripts de même origine. La réponse envoie `Cache-Control: no-store, no-transform` suivant la [documentation Cloudflare](https://developers.cloudflare.com/waf/tools/scrape-shield/email-address-obfuscation/#troubleshoot-email-obfuscation). Le test de page connectée vérifie cet en-tête et la présence de l'adresse de démonstration. La page distante non connectée a aussi été contrôlée après redémarrage avec `bin/studio.exe` ; l'affichage de l'e-mail réel après rafraîchissement reste à confirmer par l'utilisateur.

Restent notamment à tester : prénom/nom avec des champs renseignés dans Shopify, affichage de l'e-mail après le correctif du tunnel, déconnexion et changement de compte, Chrome/Safari/mobile et permissions sur la future boutique commerciale. La connexion réussie sur une dev store Basic ne valide pas à elle seule tous ces environnements.

Le domaine `example-store.myshopify.com` a ensuite été fourni. Le 15 octobre 2026, les lectures publiques suivantes ont réussi :

| Endpoint | Résultat |
| --- | --- |
| `https://example-store.myshopify.com/.well-known/openid-configuration` | HTTP 200, métadonnées OIDC, signature RS256 et PKCE S256 annoncés |
| `https://example-store.myshopify.com/.well-known/customer-account-api` | HTTP 200, endpoint GraphQL versionné annoncé |

L'utilisateur a signalé un refus d'installation de Headless sur cette boutique, avec un forfait affiché **Trial, 3 days remaining**. Cette boutique est donc un environnement d'essai commercial limité dans le temps. La présence des endpoints ne prouve ni l'éligibilité d'un client ni l'accès aux données d'un compte. Aucun token client n'a été obtenu et aucun profil réel n'a été lu.

Aucun navigateur pilotable n'était exposé à l'outil de test ; le helper natif a également échoué à se connecter. Les cookies ont été vérifiés par HTTP et par leurs attributs ; leurs règles dans Chrome, Safari et sur mobile doivent être testées sur la vraie boutique. La procédure est dans le [guide](prototype-history.md#validation-sur-la-boutique-de-développement).

L'utilisateur a autorisé la fermeture de l'ancienne boutique d'essai uniquement. Elle n'a pas pu être effectuée : la CLI ne propose la suppression que pour les dev stores et aucun navigateur pilotable n'est disponible. Le lien direct vers ses paramètres de forfait a été fourni. La nouvelle dev store et son organisation ont été conservées.
