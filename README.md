# Connecter le studio PocketBase avec les comptes clients Shopify

Enquête de faisabilité et prototype, 15 octobre 2026.

La solution retenue utilise les nouveaux comptes clients Shopify, la Customer Account API et un flux OAuth 2.0 avec OpenID Connect. Le studio crée ensuite sa propre session PocketBase. Elle ne nécessite pas Shopify Plus ni un service tiers d'authentification. Le client peut être enregistré par notre propre app Shopify avec `customer_authentication`, ou par le canal officiel Headless lorsqu'il est disponible. La boutique peut conserver son thème et son domaine actuels.

Le flux est documenté par Shopify. Ce dépôt fournit l'adaptation Go/PocketBase et ses tests. Les tests locaux utilisent un fournisseur simulé et une vraie base PocketBase. L'app est installée sur la dev store Basic non-Plus. Le parcours anonyme a été vérifié contre les vrais serveurs Shopify, puis l'utilisateur a confirmé son arrivée connecté dans le studio. Le serveur a lu le profil réel et créé le compte PocketBase associé. Voir [TESTS.md](TESTS.md).

## Faisabilité et pistes examinées

| Piste | Conclusion pour ce projet |
| --- | --- |
| Customer Account API, OAuth 2.0 et OpenID Connect | Retenue. Authentifie un client de la boutique et donne accès à son profil. Compatible avec les nouveaux comptes déjà utilisés ici. |
| Fournisseur OIDC natif de PocketBase | Disponible et testé localement. Il peut extraire l'identité du jeton. Une adaptation reste nécessaire pour enrichir le profil via GraphQL et obtenir le parcours de navigation demandé. |
| App proxy Shopify avec `logged_in_customer_id` signé | Alternative possible, surtout pour une intégration existante avec les anciens comptes. Nécessite une app et une transmission sûre vers le domaine du studio. Aucun avantage ici par rapport au flux natif. |
| Customer account UI extension et Session Token API | Alternative valable si l'on veut placer le studio ou son point d'entrée dans l'espace compte Shopify. Ajoute une extension et un mécanisme de transmission vers le studio. |
| Storefront API et `customerAccessTokenCreate` | Réservé aux anciens comptes avec mot de passe. Ne transforme pas la session navigateur existante en jeton pour le studio. Inadapté aux nouveaux comptes avec code e-mail. |
| Multipass | Exclu, car réservé à Shopify Plus. Sert principalement à faire reconnaître par Shopify une identité déjà connue d'une application externe. |
| Fournisseur d'identité externe commun aux deux sites | Le branchement natif d'un fournisseur externe aux comptes Shopify requiert Plus. Ce serait aussi un changement du système d'authentification demandé. |
| App tierce faisant de Shopify un fournisseur d'identité | Dernier recours commercial possible. Le produit doit explicitement prendre en charge le sens Shopify vers application externe, les nouveaux comptes et le forfait réel. |
| Partage de cookies, identifiant Liquid dans un lien, e-mail en paramètre | Ne fournit pas une preuve d'identité exploitable par le studio. Un visiteur peut modifier les paramètres d'un lien. Un sous-domaine commun ne donne pas accès aux cookies privés de Shopify. |
| Admin API ou OAuth d'installation d'app seuls | Authentifient une application auprès d'une boutique, pas le client présent dans le navigateur. Peuvent enrichir un profil après une preuve d'identité, mais ne remplacent pas cette preuve. |

Les nouveaux comptes doivent être activés. Pour enregistrer le client, Shopify documente deux voies : une app avec sa propre configuration `customer_authentication`, ou un client de storefront via Headless/Hydrogen. Headless n'est donc pas un prérequis universel. La documentation distingue aussi les limites des forfaits standard, Advanced et Plus. [Configuration d'app](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication), [guide Headless](https://shopify.dev/docs/storefronts/headless/building-with-the-customer-account-api/getting-started), [référence de l'API](https://shopify.dev/docs/api/customer/latest#rate-limits).

Pour l'app proxy, Shopify signe les paramètres, dont l'identifiant du client. Il faut vérifier cette signature avec le secret d'app, la boutique attendue, la fraîcheur de la requête et les paramètres ambigus. Shopify supprime les cookies entrants et sortants du proxy. Une session studio doit donc être créée sur son propre domaine, via un code court à usage unique lié au navigateur. Un identifiant client signé ne doit pas devenir un jeton réutilisable dans une URL. [Documentation des app proxies](https://shopify.dev/docs/apps/build/online-store/app-proxies/authenticate-app-proxies).

Des signalements sur le forum Shopify indiquent des identifiants de proxy parfois absents ou périmés avec les nouveaux comptes. Cela rend cette piste moins convaincante ici. Ces signalements ne prouvent pas que toutes les boutiques sont touchées. [Discussion Shopify](https://community.shopify.dev/t/logged-in-customer-id-erroneously-empty-with-new-customer-accounts-app-proxy/2204).

Une extension de compte peut envoyer à son backend un jeton Shopify signé contenant l'identité du client, lorsque les permissions le permettent. Ce jeton est propre à ce contexte d'extension. Un simple lien dans le thème n'y donne pas accès. [Session Token API](https://shopify.dev/docs/api/customer-account-ui-extensions/latest/target-apis/platform-apis/session-token-api).

Shopify confirme les restrictions des [anciens jetons Storefront](https://shopify.dev/docs/api/storefront/latest/mutations/customerAccessTokenCreate), de [Multipass](https://shopify.dev/docs/api/customer-authentication/multipass) et des [fournisseurs d'identité externes](https://shopify.dev/docs/api/customer-authentication). Les anciens comptes sont dépréciés depuis le 26 février 2026 ; ce n'est pas une bonne base pour une nouvelle intégration. [Annonce officielle](https://shopify.dev/changelog/posts/legacy-customer-accounts-are-deprecated).

miniOrange commercialise par exemple une app « Store as IDP » pour connecter des applications externes aux comptes Shopify. C'est une piste de repli, pas une solution testée dans ce projet. La compatibilité exacte avec les nouveaux comptes, les données transmises et le contrat doivent être vérifiés avant tout achat. [Présentation de l'éditeur](https://plugins.miniorange.com/how-to-use-shopify-as-an-identity-provider-for-single-sign-on-sso).

## Parcours obtenu

1. Le client ouvre la boutique et s'y connecte avec le code e-mail habituel.
2. Le lien « Studio » pointe vers `https://studio.example/studio`.
3. Sans session locale, le studio lance une navigation vers Shopify avec `prompt=none`.
4. Si Shopify reconnaît une session utilisable, il renvoie un code sans afficher de formulaire de connexion.
5. Le backend échange ce code, valide le jeton d'identité et interroge le profil client.
6. Il crée ou retrouve le compte PocketBase, pose un cookie de session studio, puis affiche le studio connecté.

Le navigateur effectue des redirections. L'absence de nouvelle saisie dépend de la session Shopify et des règles du fournisseur. Une session expirée ou une interaction exigée par Shopify empêche une entrée entièrement automatique.

Pour une arrivée directe sans session Shopify, la tentative silencieuse revient au studio, qui affiche « Se connecter avec Shopify ». Ce lien démarre le flux interactif vers la page d'authentification Shopify. Il faut utiliser cette URL OAuth complète pour conserver le retour au studio, plutôt qu'un lien isolé vers `/account/login`.

Le comportement de `prompt=none`, le retour `login_required` et la nécessité d'une navigation de premier niveau sont décrits dans la [référence d'authentification Shopify](https://shopify.dev/docs/api/customer/latest#authorization).

## Obtenir une boutique de test gratuite

Une boutique commerciale en essai et une dev store sont deux parcours distincts. Aucun abonnement commercial n'est nécessaire pour cette validation.

Un écran **Plan details > Trial > 3 days remaining** indique un essai commercial. À son expiration, la boutique est suspendue si aucun forfait n'a été choisi. Pour des tests gratuits durables, créer une boutique de type Dev depuis le Dev Dashboard. Le message d'indisponibilité de Headless sur un essai ne démontre pas une restriction à Shopify Plus. [Fonctionnement de l'essai commercial](https://help.shopify.com/en/manual/intro-to-shopify/pricing-plans/free-trial).

1. Ouvrir le [Dev Dashboard](https://dev.shopify.com/). Il faut un compte Partner ou des permissions de développement sur une organisation marchand. Si nécessaire, commencer par le [programme Shopify Partner](https://www.shopify.com/partners).
2. Choisir **Stores**, **Create store**, puis le type **Dev**.
3. Nommer la boutique, par exemple `example-store`.
4. Sélectionner **Basic** ou **Grow** comme forfait de test. Utiliser un forfait non-Plus permet de vérifier la contrainte du projet.
5. Créer la boutique et noter son domaine `*.myshopify.com`.

Si un écran impose un abonnement payant pour poursuivre, revenir au choix du type de boutique et vérifier que l'on crée bien une dev store. Les conditions d'accès et la procédure figurent dans la [documentation des dev stores](https://shopify.dev/docs/apps/build/stores/development-stores). Shopify les décrit aussi comme des [boutiques gratuites de développement](https://shopify.dev/docs/storefronts/themes/tools/development-stores).

La protection par mot de passe de la boutique de développement est distincte de la connexion d'un client. Elle peut apparaître pendant le test. Elle n'est pas la preuve que le client est connecté à son compte.

La boutique de test créée pour ce projet est `example-store.myshopify.com`. La CLI a confirmé son type `dev` et son forfait de fonctionnalités `basic`. Son [administration](https://admin.shopify.com/store/example-store) est dans l'organisation « My Store ». Notre app `Studio SSO` est enregistrée, publiée et installée en mode développement. L'ancienne boutique commerciale `example-store.myshopify.com` n'a pas été fermée : cette opération reste à effectuer par le propriétaire depuis [ses paramètres de forfait](https://admin.shopify.com/store/example-store/settings/plan). Ne pas supprimer l'organisation « My Store », qui contient aussi la nouvelle boutique.

### Test réel préparé le 15 octobre 2026

- App : [Studio SSO dans le Dev Dashboard](https://dev.shopify.com/dashboard/000000000/apps/000000000000).
- Configuration CLI : `shopify-app/studio-sso/shopify.app.toml`, validée sans erreur.
- Version publiée : `studio-sso-auth-20261015`.
- Installation confirmée par `currentAppInstallation`, avec uniquement `customer_read_customers`.
- Studio de test : <https://tunnel.example.com/studio>.
- Configuration locale : `config.local.ps1`, ignorée par Git, client public PKCE sans secret.

Les processus PocketBase, gateway et tunnel sont démarrés pour ce test. L'URL est temporaire et cesse de répondre si le tunnel ou le serveur est arrêté. Un nouveau tunnel peut produire un autre nom : modifier alors la configuration locale et le callback de l'app, valider puis redéployer.

Pour terminer la validation, ouvrir la boutique dans son navigateur, se connecter comme **client** avec le code e-mail, puis ouvrir le studio dans le même profil navigateur. La connexion à l'admin marchand n'est pas une connexion client. Refaire ensuite le test dans une fenêtre privée en ouvrant directement le studio. Ne pas communiquer les codes reçus par e-mail.

Le parcours connecté a été confirmé par l'utilisateur après correction du sujet numérique Shopify. L'e-mail est présent dans le profil réel reçu ; les prénom et nom sont vides dans ce profil de test. Leur récupération est couverte par les tests simulés mais reste à confirmer avec un compte Shopify dont ces champs sont renseignés.

Si le tunnel Cloudflare affiche `[email protected]`, il s'agit de sa réécriture des adresses dans le HTML. Le studio envoie `Cache-Control: no-store, no-transform` pour empêcher cette transformation, sans autoriser le script de décodage injecté par le proxy. L'adresse en base n'est pas remplacée. [Documentation Cloudflare](https://developers.cloudflare.com/waf/tools/scrape-shield/email-address-obfuscation/#troubleshoot-email-obfuscation).

## Préparer Shopify

La voie avec notre propre app permet d'enregistrer le client sans installer Headless. Elle utilise le mode public avec PKCE déjà pris en charge par ce prototype. Aucun secret d'app n'est envoyé pour cet échange client, et Shopify ne délivre pas de refresh token pour ce type de client. [Documentation du flux pour une app](https://shopify.dev/docs/storefronts/headless/building-with-the-customer-account-api/authenticate-customers?extension=javascript), [configuration du client](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).

1. Activer les comptes clients avec code e-mail sur la dev store.
2. Dans le [Dev Dashboard](https://dev.shopify.com/), ouvrir **Apps > Create app > Start from Dev Dashboard** et nommer l'app `Studio SSO`.
3. Dans l'app, ouvrir **Settings / App settings > Credentials** et relever son **Client ID**. Le secret affiché dans cette page n'est pas nécessaire au flux public Customer Account API.
4. Préparer l'URL HTTPS du studio suivant la section sur le tunnel ci-dessous.
5. Dans le projet d'app CLI, reprendre les champs de `shopify.app.example.toml`, y renseigner le Client ID et l'URL HTTPS. Pour ce projet, le fichier actif est `shopify-app/studio-sso/shopify.app.toml`. Le scope d'installation est `customer_read_customers`. Le callback client va dans `[customer_authentication].redirect_uris`.
6. Avec Shopify CLI, lier la configuration à l'app puis la déployer avec `shopify app deploy`. Ce déploiement enregistre la configuration Shopify, il n'héberge pas le serveur Go. La configuration peut aussi être saisie dans l'interface du Dev Dashboard si les champs correspondants sont disponibles. Le CLI ne suffit pas sans connexion au compte autorisé à gérer l'app.
7. Sur une dev store, `shopify app dev --path ./shopify-app/studio-sso --store example-store.myshopify.com --no-update` prépare l'installation et le preview. Dans le test effectué, la CLI a accordé automatiquement le scope. Pour une installation hors de ce mode de développement, utiliser **Install app** depuis la fiche de l'app et approuver les permissions demandées. Si la version change après l'installation, approuver les nouvelles permissions si Shopify le demande.
8. Configurer `SHOPIFY_CUSTOMER_CLIENT_ID` avec ce Client ID et laisser `SHOPIFY_CUSTOMER_CLIENT_SECRET` vide.

La création et l'installation de l'app suivent le [guide du Dev Dashboard](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard). Les paramètres d'authentification marchand dans `[auth]` et ceux du client dans `[customer_authentication]` ont des rôles distincts. Le prototype n'a pas besoin d'un jeton Admin API pour lire le profil du client connecté.

Dans les réglages d'accès aux données protégées de l'app, déclarer le nom et l'e-mail lorsque Shopify le demande. Un test sur une dev store ne valide pas à lui seul l'accès à ces champs sur la boutique de production. [Données client protégées](https://shopify.dev/docs/apps/launch/protected-customer-data).

### Alternative avec le canal Headless

Si le canal est disponible sur la boutique et que l'on préfère gérer un client de storefront :

1. Dans la dev store, ouvrir **Settings > Customer accounts** et activer les comptes clients avec code e-mail.
2. Installer le canal officiel [Headless](https://apps.shopify.com/headless).
3. Dans Headless, créer un storefront nommé `Studio`.
4. Ouvrir ses paramètres **Customer Account API**.
5. Choisir un client **Confidential** si cette option est disponible pour le backend Go. Un client public avec PKCE fonctionne aussi dans ce prototype.
6. Relever le **Client ID Customer Account API** et, pour un client confidential, son secret. Dans cette voie Headless, utiliser les identifiants affichés dans les paramètres du storefront.
7. Autoriser la lecture des informations du client nécessaires au studio. Le prototype demande uniquement identifiant, prénom, nom et e-mail dans sa requête.
8. Enregistrer exactement le callback HTTPS décrit ci-dessous. Pour un client public web, enregistrer également l'origine JavaScript correspondante dans Shopify.

Cette procédure suit le [guide de configuration Customer Account API](https://shopify.dev/docs/storefronts/headless/building-with-the-customer-account-api/getting-started). Installer Headless pour créer le client ne demande pas de réécrire le site en Hydrogen.

L'accès au nom et à l'e-mail dépend aussi des permissions et du type d'app. Une app publique peut nécessiter une validation des données client protégées. Les règles diffèrent pour les apps personnalisées et les anciennes apps créées dans l'admin. Ce point est particulièrement à contrôler si l'on choisit l'alternative app proxy avec Admin API. [Règles officielles](https://shopify.dev/docs/apps/launch/protected-customer-data).

## Lancer la démonstration entièrement locale

Prérequis de cette version : Go 1.27.2 minimum. Avec `GOTOOLCHAIN=auto`, Go télécharge cette version pour le projet si nécessaire. PocketBase est fixé à `v0.40.5`, `go-oidc/v3` à `v3.21.0` et `x/oauth2` à `v0.37.0`. Le `go.sum` fixe aussi les dépendances transitives. Les versions `x/net v0.61.0` et `x/crypto v0.58.0` sont utilisées pour intégrer les correctifs signalés par le contrôle de vulnérabilités.

Dans un premier terminal PowerShell :

```powershell
cd D:/Workspaces/shopify-pocketbase-sso
go run ./cmd/mockshop
```

Dans un second terminal :

```powershell
cd D:/Workspaces/shopify-pocketbase-sso
$env:SHOPIFY_SHOP_URL = 'http://127.0.0.1:8091'
$env:STUDIO_URL = 'http://127.0.0.1:8090'
$env:SHOPIFY_CUSTOMER_CLIENT_ID = 'demo-client'
$env:SHOPIFY_CUSTOMER_CLIENT_SECRET = ''
$env:SSO_ALLOW_LOOPBACK_HTTP = '1'
go run . serve --http=127.0.0.1:8090
```

Ouvrir `http://127.0.0.1:8091`, cliquer sur la connexion de Camille, puis sur le studio. Le profil doit apparaître. Dans une session privée sans ces cookies, ouvrir directement `http://127.0.0.1:8090/studio` pour voir le parcours invité.

Ce serveur simule Shopify. Il ne délivre pas de vrai code e-mail et ne valide ni l'éligibilité d'un forfait Shopify ni le partage des sessions d'une vraie boutique.

Arrêter les deux terminaux avec Ctrl+C avant de passer au test réel.

## Brancher une vraie boutique avec un callback HTTPS

Shopify exige un callback HTTPS et ne prend pas en charge `localhost` pour cette configuration. Pour le test, on peut utiliser un tunnel temporaire. [Prérequis Shopify](https://shopify.dev/docs/storefronts/headless/building-with-the-customer-account-api/getting-started).

Le dépôt contient une petite gateway qui laisse passer uniquement `/studio` et ses sous-routes. L'admin PocketBase et l'API générale restent hors de ce tunnel de test.

1. Installer `cloudflared` selon sa [documentation officielle](https://developers.cloudflare.com/tunnel/downloads/).
2. Depuis le projet, lancer `go run ./cmd/gateway` dans un terminal.
3. Dans un autre terminal, lancer :

```powershell
cloudflared tunnel --url http://127.0.0.1:8092
```

Cloudflare affiche une URL temporaire telle que `https://exemple.trycloudflare.com`. Ce mode ne demande pas de compte Cloudflare. Le nom change au redémarrage et convient au développement. [Documentation des Quick Tunnels](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/).

4. Pour notre propre app, renseigner l'URL dans `[customer_authentication].redirect_uris` du fichier `shopify.app.toml`, puis publier cette configuration. Pour la voie Headless, utiliser ses paramètres Customer Account API. L'adresse de retour est :

```text
Callback URL : https://exemple.trycloudflare.com/studio/callback
Origine JavaScript, si client public web : https://exemple.trycloudflare.com
```

Pour la voie app, les requêtes d'échange sont faites par le backend Go. Le champ `javascript_origins` n'est pas nécessaire à ce prototype.

5. Copier `config.example.ps1` vers `config.local.ps1`, puis renseigner le domaine de la boutique, l'URL HTTPS et le Client ID. Pour la voie app, garder le secret vide. Pour un client Headless confidential, utiliser la ligne `Read-Host` fournie pour saisir son secret sans le placer dans l'historique.
6. Charger la configuration et démarrer PocketBase :

```powershell
. ./config.local.ps1
go run . serve --http=127.0.0.1:8090
```

7. Dans le navigateur, ouvrir la boutique de test, se connecter comme client avec une adresse dont on peut recevoir le code, puis ouvrir `https://exemple.trycloudflare.com/studio` dans le même profil navigateur.

Les origines configurées ne doivent avoir ni chemin ni slash final. Le callback doit, lui, inclure `/studio/callback`. Ne pas activer `SSO_ALLOW_LOOPBACK_HTTP` pour le test réel. Le tunnel transmet les échanges de test via Cloudflare.

Pour le lien boutique, utiliser cette même adresse du studio dans la navigation ou le thème. En production, elle sera remplacée par un domaine stable.

## Intégrer le code au studio existant

Le package `internal/studio` contient le flux. Dans le hook `OnServe`, `studio.New` charge les métadonnées de la boutique, puis `Register` ajoute les routes. `main.go` donne un exemple complet. L'adaptation à une autre version de PocketBase doit être vérifiée avant de copier le code.

La collection d'auth `studio_customers` est créée au premier démarrage. Elle stocke l'émetteur et le sujet OIDC, l'identifiant client Shopify, l'e-mail et les noms. Les opérations publiques de création et de modification sont verrouillées. Le client peut lire son propre enregistrement. La liaison repose sur le couple émetteur et sujet, jamais sur une recherche par e-mail.

Pour une application qui possède déjà une collection `users`, prévoir une migration explicite de cette liaison et de ses relations. Ce prototype évite de modifier une collection existante.

Le serveur rend une page connectée grâce à un cookie HttpOnly. Si le frontend doit utiliser le SDK JavaScript PocketBase sur la même origine, il peut récupérer le jeton local ainsi :

```javascript
const response = await fetch('/studio/session', {
  method: 'POST',
  credentials: 'same-origin',
});
if (!response.ok) throw new Error('Connexion Shopify requise');
const { token, record } = await response.json();
pb.authStore.save(token, record);
```

Ce jeton est un jeton PocketBase, utilisable par ses règles d'accès. Il ne remplace pas un jeton Shopify. La gateway de test ne publie pas `/api`; pour utiliser le SDK depuis un navigateur distant, le déploiement final devra aussi desservir les routes PocketBase voulues. La [documentation PocketBase](https://pocketbase.io/docs/authentication/) explique les sessions SDK et les méthodes OAuth natives.

Les noms proviennent de la requête `customer` de la Customer Account API. Les champs sont documentés dans l'objet [Customer](https://shopify.dev/docs/api/customer/latest/objects/Customer). Ils peuvent être vides si le client ne les a pas renseignés.

## Limites du prototype à conserver en tête

La session PocketBase dure 15 minutes et n'est pas renouvelable par `auth-refresh`. Une nouvelle entrée dans le studio peut relancer Shopify sans saisie si sa session reste active. Les jetons Shopify, y compris un éventuel refresh token, sont abandonnés après la lecture du profil. Ce choix évite leur stockage dans ce prototype et applique KISS au besoin actuel.

Les informations de profil sont synchronisées à chaque authentification Shopify du studio. Elles ne sont pas actualisées en continu pendant une session locale.

La déconnexion du studio efface son cookie. Elle ne déconnecte pas Shopify. Une déconnexion ou un changement de compte sur la boutique ne modifie pas immédiatement une session studio existante. Un jeton PocketBase déjà copié reste utilisable jusqu'à son expiration. Une déconnexion globale immédiate demanderait une gestion de session supplémentaire.

Les tentatives OAuth durent cinq minutes et sont stockées en mémoire du processus. Un redémarrage annule les connexions en cours. Ce stockage suppose une instance, ou des sessions de routage affinitaires ; pour plusieurs instances, le remplacer par un stockage partagé à consommation atomique. Deux connexions simultanées dans deux onglets peuvent obliger à recommencer la première.

Le prototype utilise une collection dédiée avec Shopify comme source d'authentification. Il ne fournit pas le parcours MFA ni les alertes de connexion propres à PocketBase. Une politique de sécurité existante sur `users` doit être intégrée explicitement.

Les callbacks ne passent pas dans le journal d'activité PocketBase et le mode SQL debug est désactivé par défaut. Au déploiement, appliquer la même exclusion aux logs du reverse proxy et configurer les limites de requêtes. Le code valide les signatures OIDC avec JWKS, l'émetteur, l'audience, l'expiration, le nonce, le state et PKCE. [Bibliothèque go-oidc](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc), [bibliothèque OAuth Go](https://pkg.go.dev/golang.org/x/oauth2).

### Compatibilité avec les sujets Shopify numériques

Le test réel a révélé un jeton dont `sub` est un nombre JSON, alors que `go-oidc` attend une chaîne. Shopify a reconnu un signalement équivalent et indiqué mener une investigation le 12 octobre 2026. Ce n'est pas une propriété garantie de tous les jetons Shopify. [Discussion officielle](https://community.shopify.dev/t/customer-account-sign-in-fails-numeric-sub-in-id-token-payload/38544), [type prévu par OpenID Connect](https://openid.net/specs/openid-connect-core-1_0.html#IDToken).

L'adaptateur `shopifySubjectKeySet` s'exécute dans le vérificateur `go-oidc`. La bibliothèque filtre d'abord l'algorithme, puis l'adaptateur fait vérifier la signature du JWT original par le JWKS. Il convertit seulement ensuite un `sub` entier positif en sa chaîne décimale exacte pour le décodage des claims. Il ne convertit pas les autres champs et ne passe jamais par un nombre flottant. Les fractions, exposants, nombres négatifs et types inattendus sont refusés. Les sujets déjà textuels restent inchangés.

Le vérificateur conserve ses contrôles d'émetteur, d'audience, d'expiration et de `nbf`, puis le callback vérifie le nonce et `at_hash` s'il est présent. Aucun mode `Skip*` n'est activé. Cette adaptation dépend du fonctionnement de la version `go-oidc v3.21.0` fixée dans le projet. Conserver les tests de non-régression lors de toute mise à jour de cette dépendance.

## Validation sur la boutique de développement

Vérifier les cas suivants avec les mêmes domaines et le forfait non-Plus choisis pour le projet :

- Boutique connectée, aucune session studio : arrivée connectée au studio sans nouveau code e-mail.
- Arrivée directe dans un profil navigateur privé : affichage du lien, connexion Shopify, puis retour connecté au studio.
- Nom ou e-mail modifié dans Shopify : données mises à jour après une nouvelle authentification du studio.
- Session Shopify expirée : le studio propose la connexion, sans boucle de redirection.
- Déconnexion studio, changement de client dans Shopify et session studio déjà ouverte : comportement conforme aux limites décrites ci-dessus.
- Chrome et Safari, puis un téléphone, pour vérifier les cookies et redirections dans les navigateurs réellement utilisés.

Les éléments nécessaires à ce test sont le domaine public de la dev store, le Client ID de l'app ou du client Headless retenu, une URL HTTPS enregistrée et un compte client de test. Le secret n'est requis que pour la voie Headless confidential. Tout secret et le code reçu par e-mail doivent rester dans la configuration locale ou être saisis par le propriétaire du compte.
