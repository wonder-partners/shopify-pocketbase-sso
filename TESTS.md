# Résultats de validation

Exécution le 15 octobre 2026, Windows amd64, Go 1.27.2 et PocketBase 0.40.5.

Le prototype passe les tests locaux. L'app est installée sur une vraie dev store Basic non-Plus. Le parcours anonyme a été vérifié contre Shopify et l'utilisateur a ensuite confirmé son arrivée connecté dans le studio. Le serveur a récupéré le profil réel et créé le compte PocketBase associé.

## Tests automatisés

```powershell
go test ./... -count=1 -cover
go vet ./...
```

Résultat : succès. Après ajout des tests des diagnostics et des sujets numériques Shopify, le package `internal/studio` contient 19 groupes de tests. Sa couverture d'instructions est de 87,7 % avec Go 1.27.2. Le code des commandes de lancement n'est pas couvert par ce pourcentage.

| Scénario | Résultat |
| --- | --- |
| Session boutique existante, client public avec PKCE | Passe |
| Session boutique existante, client confidential avec HTTP Basic | Passe |
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
| Modification publique de l'identité Shopify ou inscription directe | Bloquées |
| Appel de l'API PocketBase avec le jeton émis | Autorisé pour son propre profil |
| Renouvellement via `auth-refresh` | Le jeton reste identique, durée non prolongée |
| Jeton PocketBase signé mais expiré | Rejeté |
| Requête de session ou de déconnexion depuis une autre origine | Bloquée |
| Déconnexion locale | Cookie supprimé, session locale absente |
| Transaction de connexion expirée | Rejetée |
| Configuration HTTP en dehors du loopback explicitement autorisé | Rejetée |
| Cookie en configuration HTTPS | Secure, HttpOnly, SameSite=Lax, préfixe `__Host-`, sans Domain |
| Fournisseur OIDC natif de PocketBase | Extraction de l'identité et validation JWKS vérifiées |

Les tests utilisent des serveurs HTTP réels sur des ports locaux aléatoires, une clé RSA et son JWKS, des codes d'autorisation à usage unique, un cookie de boutique simulée et une base SQLite PocketBase distincte par test. Le faux endpoint de jeton vérifie le challenge PKCE et, lorsque configuré, le secret du client confidential. Le faux endpoint GraphQL exige l'access token brut attendu par Shopify.

Le serveur simulé est sous notre contrôle. Ces tests vérifient le comportement du client et de PocketBase face au contrat simulé ; ils ne constituent pas un test des serveurs Shopify.

## Exécutables et vérification HTTP manuelle

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

## Validation encore nécessaire

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

L'utilisateur a signalé `[email protected]` à l'écran. La base ne contient pas ce texte et contient bien une adresse avec `@`. Cloudflare documente cette réécriture automatique du HTML et l'injection d'un script de décodage ; la politique CSP du studio bloque les scripts. La réponse envoie désormais `Cache-Control: no-store, no-transform` suivant la [documentation Cloudflare](https://developers.cloudflare.com/waf/tools/scrape-shield/email-address-obfuscation/#troubleshoot-email-obfuscation). Le test de page connectée vérifie cet en-tête et la présence de l'adresse de démonstration. La page distante non connectée a aussi été contrôlée après redémarrage avec `bin/studio.exe` ; l'affichage de l'e-mail réel après rafraîchissement reste à confirmer par l'utilisateur.

Restent notamment à tester : prénom/nom avec des champs renseignés dans Shopify, affichage de l'e-mail après le correctif du tunnel, déconnexion et changement de compte, Chrome/Safari/mobile et permissions sur la future boutique commerciale. La connexion réussie sur une dev store Basic ne valide pas à elle seule tous ces environnements.

Le domaine `example-store.myshopify.com` a ensuite été fourni. Le 15 octobre 2026, les lectures publiques suivantes ont réussi :

| Endpoint | Résultat |
| --- | --- |
| `https://example-store.myshopify.com/.well-known/openid-configuration` | HTTP 200, métadonnées OIDC, signature RS256 et PKCE S256 annoncés |
| `https://example-store.myshopify.com/.well-known/customer-account-api` | HTTP 200, endpoint GraphQL versionné annoncé |

L'utilisateur a signalé un refus d'installation de Headless sur cette boutique, avec un forfait affiché **Trial, 3 days remaining**. Cette boutique est donc un environnement d'essai commercial limité dans le temps. La présence des endpoints ne prouve ni l'éligibilité d'un client ni l'accès aux données d'un compte. Aucun token client n'a été obtenu et aucun profil réel n'a été lu.

Aucun navigateur pilotable n'était exposé à l'outil de test ; le helper natif a également échoué à se connecter. Les cookies ont été vérifiés par HTTP et par leurs attributs ; leurs règles dans Chrome, Safari et sur mobile doivent être testées sur la vraie boutique. La procédure est dans le [guide](README.md#validation-sur-la-boutique-de-développement).

L'utilisateur a autorisé la fermeture de l'ancienne boutique d'essai uniquement. Elle n'a pas pu être effectuée : la CLI ne propose la suppression que pour les dev stores et aucun navigateur pilotable n'est disponible. Le lien direct vers ses paramètres de forfait a été fourni. La nouvelle dev store et son organisation ont été conservées.
