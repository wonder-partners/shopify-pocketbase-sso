# Passation technique Codex vers T3 Code

## cloudflared géré par mise, 16 octobre 2026

Contributor a demandé pourquoi les binaires étaient dans le projet et proposé leur gestion par mise. Inventaire : `cloudflared.exe` était un outil téléchargé, les autres exécutables sont des builds locaux des différentes étapes du prototype et le build actuel `studio-sso.exe`. Ces builds ne sont pas des dépendances installables par mise.

`mise.toml` déclare désormais `cloudflared = "2026.10.1"`, identique à la version de la copie locale, et une tâche `tunnel` qui exécute `cloudflared tunnel --url http://127.0.0.1:8090`. Mise l'a installé hors du dépôt dans son stockage utilisateur. Version et résolution du chemin vérifiées ; empreinte SHA-256 identique à l'ancienne copie. Après vérification qu'aucun processus n'utilisait cette copie, `bin/cloudflared.exe` a été retiré. Les anciens builds du prototype sont conservés, et `mise run build` continue de produire le binaire actuel dans `bin/`, ignoré par Git.

README mis à jour. `mise tasks validate` valide les neuf tâches et `mise run --dry-run tunnel` produit la commande attendue. Aucun tunnel ni serveur n'a été lancé pour cette migration. Pour lancer maintenant : `mise run dev` et, dans un autre terminal, `mise run tunnel`. Les anciennes commandes utilisant `./bin/cloudflared.exe` sont historiques. L'alignement des URL OAuth reste à faire avant le test réel.

## Suppression du mode confidential, 16 octobre 2026

Contributor a explicitement demandé de retirer ce reliquat du code, de la configuration et de la documentation. L'intégration prend maintenant exclusivement en charge le client public PKCE de notre app Shopify.

- `Config.ClientSecret`, sa lecture dans `main.go`, sa variable mise et le choix d'authentification Basic ont été supprimés. Le client OAuth d'échange a un Client ID et `AuthStyleInParams`, sans secret. Une ancienne variable d'environnement de secret n'est plus lue.
- Le marqueur public `public-pkce-no-secret` reste fixé dans la configuration native du fournisseur, uniquement parce que PocketBase v0.40.5 exige un champ non vide. Il ne participe jamais à l'échange Shopify. Ne pas le confondre avec un credential à fournir.
- Le simulateur et les fixtures n'ont plus de paramètre de secret ni de branche confidential. Les tests existants utilisent tous PKCE. Le simulateur refuse tout paramètre `client_secret`, même vide, et tout en-tête `Authorization` au token endpoint, ce qui vérifie aussi que le marqueur PocketBase ne fuit pas vers Shopify. Les secrets propres aux autres fournisseurs restent préservés et testés.
- Le fichier local ne contenait que l'ancienne affectation vide ; elle a été retirée et le fichier désormais inutile supprimé. `mise.local.toml` reste ignoré pour d'éventuelles surcharges personnelles, sans obligation d'en créer un. Les quatre variables publiques restent dans `mise.toml`. Aucun nouveau fichier d'exemple.
- README et TESTS décrivent le client public uniquement. Les archives identifient le test confidential comme supprimé et leurs instructions de configuration ne demandent plus de secret. Les sections de migration ci-dessous sont historiques et ne doivent pas réintroduire cette option.

Validation : `mise run check` réussi avec les trois intégrations SDK, l'analyse Go, le build et les contrôles JS/diff. Couvertures : hôte 48,4 %, cœur 86,5 %, migrations 81,0 %. La variation de couverture du cœur suit le retrait de la branche et des scénarios confidential. `mise tasks validate` passe aussi sans fichier local. Aucun frontend, règle d'auth, compte, liaison, dépendance ou callback externe modifié ; aucune base réelle ouverte par un serveur.

## Historique remplacé : déclaration du secret optionnel sans modèle

Contributor a questionné l'utilité de `mise.local.example.toml` et proposé les déclarations `required` de mise. Vérification de la documentation officielle : `required` vérifie qu'une valeur est fournie par l'environnement ou un autre fichier ; il ne fournit pas de valeur et ne se combine pas avec `default` ou `value`.

Dans notre cas, le seul paramètre privé, `SHOPIFY_CUSTOMER_CLIENT_SECRET`, est facultatif pour le client public PKCE. Il est donc déclaré dans `[env]` de `mise.toml` avec `{ default = "", redact = true }`. Les valeurs publiques restent définies directement. `mise.local.example.toml` est supprimé. Le vrai `mise.local.toml` ignoré est conservé et peut fournir le secret pour un client confidential. Aucun `required` artificiel n'est ajouté au secret ; une future variable réellement obligatoire pourra utiliser cette déclaration. README mis à jour. Ces instructions remplacent les références aux modèles des sections historiques ci-dessous.

## Historique : déplacement des variables publiques dans mise.toml

À la demande explicite de Contributor, les valeurs non sensibles existantes sont maintenant directement dans `[env]` de `mise.toml` : `SHOPIFY_SHOP_URL`, `SHOPIFY_REDIRECT_URL`, `SHOPIFY_CUSTOMER_CLIENT_ID` et `SSO_ALLOW_LOOPBACK_HTTP`. Les doublons ont été retirés de `mise.local.toml`. Ce fichier ignoré et son modèle public ne contiennent plus que `SHOPIFY_CUSTOMER_CLIENT_SECRET`, vide pour notre client public PKCE. Aucun secret n'a été ajouté à la configuration partagée.

L'environnement résolu par `mise exec` a été comparé en mémoire avant/après : les cinq valeurs sont strictement identiques, sans affichage. `mise tasks validate` passe. README et modèle local sont à jour. Aucun changement applicatif, de callback, de données ou de configuration Shopify externe. Cette répartition remplace celle de la section de migration ci-dessous.

## Nettoyage du tooling avec mise, 16 octobre 2026

Contributor a demandé de remplacer la gestion manuelle des outils, variables et tâches par mise-en-place. Cette section remplace les instructions historiques de configuration/lancement PowerShell présentes plus bas.

- État de départ constaté : arbre Git propre, HEAD `ee66871` (`Add minimal Shopify SSO test frontend`). Le frontend précédent est donc désormais commité. `shopify-app/` existe mais est vide ; ne pas recréer son ancien scaffold ou sa configuration depuis l'historique.
- mise est déjà installé sur le poste, version `2026.10.11`. `mise.toml` fixe Go `1.27.2` et Node `24.14.1`, sans changement de version du projet. npm `11.11.0` est fourni par ce Node. Go était déjà installé par mise ; Node a été installé par mise pendant cette étape. Aucun changement du profil shell ou de la configuration globale n'a été effectué.
- Les cinq affectations constantes de l'ancien `config.local.ps1` ont été transférées vers `[env]` dans `mise.local.toml`, puis comparées aux variables réellement chargées par `mise exec` en mémoire, sans affichage des valeurs. Égalité confirmée, puis ancien fichier local retiré. `mise.local.toml` est ignoré par Git. Le modèle public est désormais `mise.local.example.toml` ; `config.example.ps1` est supprimé. Les anciens patterns d'ignore de credentials sont conservés.
- `SSO_ALLOW_LOOPBACK_HTTP` vaut `0` par défaut dans la configuration partagée. Le secret client est déclaré pour masquage par mise. Ne pas imprimer `mise env` ou le contenu de `mise.local.toml` dans les logs/chat. Le callback local migré reste inchangé, sur l'ancien chemin ; le nettoyage n'aligne pas encore les callbacks OAuth.
- Tâches : `setup` pour modules Go et `npm ci` du SDK fixé ; `dev` pour lancer Go/PocketBase et le frontend avec des chemins absolus sous la racine du projet ; `test` avec dépendance `setup` et `RUN_JS_SDK_TEST=1` ; `build` vers `bin/` ; `vet`, `check:js`, `check:diff` ; `check` pour la validation complète. Les tâches utilisent le shell natif par défaut de mise et n'exigent pas d'activation du profil. Aucun nouveau script de wrapper.
- README et TESTS utilisent maintenant les commandes mise. Les messages de tests ignorés orientent vers `mise run test`. Le SDK de test reste une dépendance npm fixée dans `integration`, et le navigateur continue d'utiliser le CDN. Aucun changement de logique OAuth, du frontend, des dépendances applicatives ou des données.

Validation : `mise tasks validate`, `mise install` et `mise run check` ont réussi. La suite complète avec les trois intégrations SDK passe. Couvertures inchangées : hôte 48,4 %, cœur 86,8 %, migrations 81,0 %. Build réussi dans `bin/`. La tâche `dev` n'a pas été lancée sur la base réelle. Aucune publication externe ni modification Shopify.

Commandes actuelles : `mise install`, `mise run setup`, `mise run check`, puis `mise run dev` seulement au moment du test réel, avec les URL alignées. Pour une nouvelle installation, créer `mise.local.toml` à partir du modèle avant le login réel ; sur ce poste il est déjà migré, ne pas l'écraser.

Prochaine tâche fonctionnelle inchangée : accès HTTPS de test et alignement du callback frontend `/auth/callback`, de `SHOPIFY_REDIRECT_URL` et de l'app Shopify, puis test réel avec Contributor. Vérifier d'abord où se trouve maintenant le projet Shopify, puisque son ancien dossier est vide.

## Implémentation frontend autorisée le 16 octobre 2026

Cette section remplace les constats d'absence du frontend et d'attente de validation ci-dessous. Contributor a autorisé la reprise du frontend minimal à partir de « Manual code exchange » de la documentation PocketBase, puis demandé explicitement un CDN pour le SDK, sans copie locale, et tout le CSS/JavaScript inline dans le HTML. Ces demandes remplacent le plan historique de fichiers JS/CSS/vendor séparés.

- `main.go` sert maintenant `pb_public` via `apis.Static`, avec un callback exact `/auth/callback`, sans fallback HTML global. Le service statique reste dans ce fichier. `--publicDir` est explicite et relatif au répertoire de travail par défaut.
- `pb_public` ne contient que `index.html`, avec CSS et JavaScript inline. Le SDK est importé depuis jsDelivr avec la version fixée `0.28.1`. Aucune copie locale du SDK n'est livrée. CSP adaptée au fichier CDN précis et aux empreintes SHA-256 des deux blocs inline, `no-store` et `no-referrer` sur le frontend. Après changement d'un bloc inline, recalculer son empreinte CSP dans `main.go` ; le test des routes vérifie leur correspondance, avec normalisation CRLF/LF comme le navigateur.
- Les fonctions OAuth sont identiques à celles du README et cette parité est testée. Tentative silencieuse initiale, bouton interactif, callback consommé une fois, adresse nettoyée avant échange, absence de boucle, refresh puis lecture API native avec le même SDK, restauration de l'auth store et logout local. MFA signalée sans interface complète. Aucun credential affiché.
- `main_test.go` vérifie routes API/admin et frontend, protections, empreintes inline, parité et version du SDK. `integration/frontend.mjs` exécute le vrai module inline de la page avec le SDK officiel et les serveurs simulés, en simulant DOM, navigation et stockage. Les trois tests hôte passent avec le flag SDK. L'affichage MFA/conflit est vérifié par réponses injectées ; leur application serveur reste couverte par les tests du cœur.
- Aucun changement dans `internal/studio`, les dépendances, la base réelle, la configuration locale ou l'app Shopify. Les callbacks de configuration restent sur `/studio/callback`. Le test réel exige encore leur alignement avec `/auth/callback` et un accès HTTPS adapté.

Validation terminée : suite complète `RUN_JS_SDK_TEST=1 go test ./... -count=1 -cover` réussie, avec les trois intégrations SDK au total. Couvertures : hôte 48,4 %, cœur 86,8 %, migrations 81,0 %. `go vet ./...`, `go build ./...`, syntaxe des trois scripts Node et `git diff --check` réussis. La version finale inline a été vérifiée dans le navigateur collaboratif T3 avec la CSP par empreintes et le SDK effectivement chargé depuis le CDN. Échange OAuth simulé, refresh et lecture API confirmés. La version CDN précédente a aussi permis de vérifier rechargement, logout local et reconnexion silencieuse. Aucun test Shopify réel, Safari ou appareil mobile n'est revendiqué.

Les serveurs de fixture sur ports éphémères ont été arrêtés et le helper de test temporaire retiré. Aucun serveur/tunnel réel n'a été démarré. README et TESTS sont à jour. Les changements restent non commités sur HEAD `75d85a7` ; préserver `shopify-app/` et les autres fichiers locaux préexistants.

Prochaine action : choisir l'accès HTTPS de test et aligner le callback frontend `/auth/callback`, `SHOPIFY_REDIRECT_URL` et le callback enregistré dans l'app Shopify, puis vérifier les scénarios sur la vraie boutique avec Contributor. Aucune publication Shopify n'a été effectuée pendant cette étape.

## Reprise T3 Code du 16 octobre 2026, inspection terminée

Contributor demande de poursuivre la session après lecture intégrale et vérification du projet. Il exige sa validation avant toute modification du code et demande la mise à jour de ce document à chaque étape majeure.

- Lecture intégrale de la passation, puis inspection de README, TESTS, de l'hôte Go, des sept fichiers du cœur, des tests concernés, de l'intégration OAuth JavaScript et du programme officiel PocketBase fixé en dépendance.
- Branche `main`, HEAD `75d85a7`. Aucun changement suivi. Les seuls éléments non suivis restent ce document et `shopify-app/`.
- Absence confirmée de `pb_public/`, `frontend.go`, `frontend_test.go` et de route statique dans `main.go`. Le programme officiel enregistre explicitement `apis.Static` ; notre hôte personnalisé doit le faire aussi. Un fichier `frontend.go` séparé est facultatif.
- Aucun processus du projet ou Cloudflare, ni listener sur 8090, 8091 ou 8092 lors de cette inspection.
- Les chemins des callbacks local et Shopify sont toujours `/studio/callback`. Le README utilise `/auth/callback` comme exemple. Aucun changement de configuration ni publication Shopify effectué.
- Nouvelle exécution réussie de `RUN_JS_SDK_TEST=1 go test ./... -count=1 -cover`, avec les deux intégrations SDK. Couvertures : cœur 86,8 %, migrations 81,0 %. `go vet ./...`, les deux contrôles de syntaxe Node et `git diff --check` passent également. Aucun build séparé, test navigateur ou test réel Shopify effectué pendant cette reprise. La base locale n'a pas été réinspectée ni le backend démarré.

Dernière tâche toujours ouverte : fournir le frontend hôte minimal permettant de tester réellement le contrat natif final. Proposition soumise à Contributor : enregistrer simplement le service statique dans `main.go`, créer la page dans `pb_public` en reprenant le parcours README, puis tester le callback, l'auth store et l'accès API natif. L'alignement des trois URL et le test HTTPS réel viendront ensuite.

État au terme de cette étape : seul ce document a été modifié. Attente de la validation explicite de Contributor avant toute modification du code. Les contraintes et décisions décrites ci-dessous restent applicables.

Vérification complémentaire à la demande de Contributor : relecture en ligne de https://github.com/pocketbase/pocketbase/discussions/132. La réponse initiale confirme le service automatique pour l'exécutable distribué. Le commentaire du 17 septembre 2022 précise explicitement qu'en usage framework il faut enregistrer le service statique. Ce projet utilise cet usage framework via son propre `main.go` ; la conclusion précédente est confirmée. Aucun code modifié, validation de Contributor toujours attendue.

Date de vérification : 16 octobre 2026, matin, fuseau Europe/Paris.

Projet : `D:/Workspaces/shopify-pocketbase-sso`.

Interlocuteur : Contributor. Échanger en français.

## À lire avant toute action

Le cœur d'authentification est présent et ses tests passent. En revanche, **le frontend minimal demandé en fin de session n'est pas présent dans l'arbre de travail inspecté**. Il n'existe actuellement ni `pb_public/`, ni `frontend.go`, ni `frontend_test.go`. `main.go` n'enregistre aucun handler statique. Aucun serveur du projet ni tunnel Cloudflare n'est actif lors de cette passation.

Une étape de création de ces fichiers et de validation locale est rapportée dans le contexte de reprise conservé de la session. Un binaire et une sauvegarde portant les noms de cette étape existent encore. Cela ne prouve pas que leurs sources sont disponibles. La cause de cet écart n'a pas été établie. Ne pas supposer un reset volontaire, ne pas reconstruire silencieusement cette étape et ne pas présenter son frontend comme livré dans HEAD.

La dernière question fonctionnelle de Contributor était :

> apparemment pb_public est servi automatiquement non ? : https://github.com/pocketbase/pocketbase/discussions/132

La réponse vérifiée est la suivante : le binaire officiel PocketBase enregistre le service de `pb_public`, mais ce projet utilise PocketBase comme framework Go. Son programme hôte doit enregistrer le handler statique. Cela peut tenir dans `main.go`. Un fichier séparé `frontend.go` n'est pas une exigence de PocketBase et ne doit pas devenir une couche d'authentification supplémentaire.

La tâche de cette passation est documentaire uniquement. **Aucun fichier source n'a été modifié pour la produire.** Le seul fichier ajouté est ce document. Ne pas confondre le plan de continuation ci-dessous avec du code déjà implémenté.

## Provenance et niveau de certitude

Le document distingue quatre niveaux :

- **Vérifié maintenant** : constat dans les fichiers, Git, la base locale ouverte en lecture seule, les processus, les outils installés ou les tests exécutés le 16 octobre 2026.
- **Historique attesté** : décision explicite de Contributor, confirmation utilisateur dans le contexte disponible, commit local ou résultat rapporté par les archives du dépôt. Ce n'est pas une nouvelle validation contre Shopify.
- **Rapporté, non présent** : travail décrit dans le contexte conservé de la session mais absent de l'arbre actuel, notamment le frontend de test de fin de session.
- **À faire / à confirmer** : intention, proposition ou test restant. Aucune réussite n'est supposée.

L'intégralité des anciennes réponses et sorties d'outils de la conversation initiale n'est pas disponible. Le contexte accessible contient les demandes et plusieurs confirmations utilisateur, un état de reprise de session, les fichiers actuels et les commits. Les archives `docs/prototype-history.md` et `docs/prototype-tests.md` complètent cet historique, mais décrivent aussi des fonctionnalités supprimées. Les sources actuelles font foi pour le contrat actuel.

---

## 1. Présentation du projet

### 1.1 Objectif général

Permettre à une application externe appelée le studio, construite en Go avec PocketBase, d'authentifier les clients d'une boutique Shopify sans passer sur Shopify Plus et sans remplacer l'authentification Shopify par un service tiers.

Parcours cible :

1. Le client arrive sur la boutique et se connecte avec les nouveaux comptes clients Shopify, par code reçu par e-mail.
2. Il suit un lien vers le studio.
3. Si sa session Shopify est utilisable, une navigation OAuth de premier niveau avec `prompt=none` permet son entrée sans nouvelle saisie.
4. Si un visiteur arrive directement au studio sans session utilisable, le frontend lui propose une connexion Shopify interactive, avec retour au studio.
5. Le studio dispose d'un compte `users` et d'un token PocketBase ordinaires. Les appels API, règles, hooks, renouvellements et abonnements realtime suivent le fonctionnement natif de PocketBase.

Le SSO ne signifie pas partage de cookies entre les domaines. Shopify authentifie le client, puis PocketBase émet sa propre authentification. Une session Shopify et une session PocketBase restent indépendantes.

### 1.2 Périmètre réellement implémenté

Le dépôt contient une adaptation Go qui ajoute un fournisseur `shopify` au parcours OAuth2 natif de PocketBase.

Le frontend attendu appelle `listAuthMethods()`, conserve les paramètres temporaires dans son `sessionStorage`, redirige vers Shopify, vérifie le retour puis appelle `authWithOAuth2Code()` du SDK officiel. Le backend échange le vrai code Shopify, valide l'ID token et lit le profil dans la Customer Account API. PocketBase assure la création ou récupération du compte et sa réponse d'authentification.

Le cœur ne sert actuellement aucune page, aucun callback HTML, aucun SDK JavaScript et aucun endpoint de session parallèle. Le README contient un exemple frontend exécuté par un test d'intégration.

### 1.3 Stack et versions vérifiées

| Élément | Version / choix actuel | Usage |
| --- | --- | --- |
| OS | Windows amd64 | Environnement de développement |
| Shell | PowerShell | Commandes locales |
| Go | `go1.27.2 windows/amd64` | Compilation et tests |
| Module Go | `studio-sso` | Nom local conservé, non préparé pour publication |
| PocketBase | `github.com/pocketbase/pocketbase v0.40.5` | Framework, API, collections, auth, hooks, realtime |
| DBX | `github.com/pocketbase/dbx v1.12.0` | Requêtes paramétrées PocketBase |
| OIDC | `github.com/coreos/go-oidc/v3 v3.21.0` | Vérification des ID tokens et JWKS |
| OAuth2 | `golang.org/x/oauth2 v0.37.0` | Échange de code et PKCE |
| JWT | `github.com/golang-jwt/jwt/v5 v5.3.1` | Notamment signature des jetons du fournisseur simulé |
| SQLite | Dépendance PocketBase, `modernc.org/sqlite v1.60.1` | Base locale et bases temporaires de test |
| Node.js | `v24.14.1` | Tests du SDK officiel et contrôles JS |
| npm | `11.11.0` | Installation des dépendances d'intégration |
| SDK JS PocketBase | `0.28.1`, fixé dans `integration/package.json` | Tests réels du SDK, auth store et API |
| Shopify CLI | `4.9.4` | Configuration de l'app Shopify et préparation des tests réels |
| API Shopify dans les fixtures / TOML | `2026-10` | Le backend de production découvre l'URL GraphQL au démarrage |
| `CGO_ENABLED` observé | `0` | Les tests ordinaires fonctionnent sans compilateur C |

`go.mod` et `go.sum` fixent les dépendances. Ne pas actualiser les versions au passage sans besoin et sans repasser les tests de compatibilité.

### 1.4 Organisation des dossiers

Tous les chemins relatifs de ce document sont relatifs à `D:/Workspaces/shopify-pocketbase-sso`. Par exemple `internal/studio/provider.go` désigne exactement `D:/Workspaces/shopify-pocketbase-sso/internal/studio/provider.go`.

```text
D:/Workspaces/shopify-pocketbase-sso/
├── main.go                         # Hôte PocketBase minimal, pas de frontend actuel
├── go.mod / go.sum
├── config.example.ps1              # Exemple public de configuration
├── config.local.ps1                # Présent, ignoré, ne pas publier son contenu
├── README.md                       # Contrat actuel et exemple frontend testé
├── TESTS.md                        # Validation du parcours actuel
├── CODEX_HANDOFF.md                # Ce document
├── internal/
│   ├── studio/                     # Adaptation Shopify/PocketBase de production
│   │   ├── studio.go
│   │   ├── provider.go
│   │   ├── shopify.go
│   │   ├── store.go
│   │   ├── accounts.go
│   │   ├── auth_response.go
│   │   └── *_test.go
│   └── mockshop/
│       └── mockshop.go             # Fournisseur simulé réservé aux tests
├── integration/
│   ├── package.json / package-lock.json
│   ├── oauth.mjs                   # Exécute l'exemple README avec le SDK officiel
│   ├── sdk.mjs                     # Compare API Shopify et compte natif
│   └── node_modules/               # Présent, ignoré
├── migrations/                     # Deux migrations historiques, non importées par main
├── docs/
│   ├── prototype-history.md        # Archive, ne pas utiliser comme contrat actuel
│   └── prototype-tests.md          # Archive des validations et du test Shopify réel
├── shopify-app/
│   └── studio-sso/                 # Projet CLI séparé, dépôt Git imbriqué non commité
├── pb_data/                        # Données locales réelles, ignorées
├── pb_backups/                     # Sauvegardes locales, ignorées
├── bin/                            # Binaires de plusieurs étapes, ignorés
└── state/                          # Répertoire présent, aucun fichier listé à l'inspection
```

Absents à cette passation : `pb_public/`, `frontend.go`, `frontend_test.go`, `cmd/gateway/`, `cmd/mockshop/`, `internal/studio/web/`, `shopify.app.example.toml`.

### 1.5 Contraintes à conserver

- Aucun passage à Shopify Plus. C'est une exclusion explicite, pas une préférence négociable implicite.
- Nouveaux comptes clients Shopify avec code e-mail. Ne pas bâtir la solution autour d'un mot de passe Shopify.
- Ne pas introduire un fournisseur d'identité tiers pour remplacer Shopify. Une app Shopify créée par le projet sert à enregistrer le client, ce n'est pas un service tiers de login.
- Utiliser `users`, conserver les politiques du projet hôte et ne donner aucun droit API spécial au compte Shopify.
- Aucun prénom ou nom Shopify à demander, ajouter ou synchroniser.
- Identifiants Shopify bruts exclusivement dans `_shopifyIdentities`, collection système verrouillée.
- Aucun stockage durable ou exposition des tokens Shopify dans l'état actuel.
- Éviter les couches de démonstration dans le cœur. Un frontend minimal de validation appartient à l'hôte, pas au module d'auth.
- La préparation du packaging et la publication comme dépendance sont explicitement différées.

## 2. Objectifs et demandes de Contributor

### 2.1 Évolution des demandes

| Étape | Demande / constat | Issue connue |
| --- | --- | --- |
| Faisabilité | Explorer les voies pour partager l'identité client Shopify avec le studio sans Plus | Customer Account API + OAuth2/OIDC retenus ; autres voies consignées dans les archives |
| Boutique de test | Contributor n'avait d'abord qu'un compte Shopify, puis une boutique avec Headless indisponible | Le forfait signalé était un essai commercial, pas la dev store finalement utilisée |
| Environnement Shopify | Contributor a installé Shopify CLI et demandé de créer la boutique | Les archives rapportent la création de `example-store.myshopify.com`, type dev, fonctionnalités Basic non-Plus |
| Ancienne boutique | Supprimer uniquement l'ancienne boutique inutile | Les archives disent que la fermeture n'a pas pu être effectuée ; ne pas affirmer qu'elle est supprimée et ne pas relancer cette opération sans instruction |
| Premier prototype | Connexion réelle en erreur `Invalid Shopify ID token` puis `verification_failed` | Diagnostic affiné ; sujet `sub` numérique identifié ; adaptation après vérification de signature ajoutée |
| Profil réel | Contributor confirme arriver connecté, mais voit un e-mail masqué dans le HTML | L'archive attribue le masquage à une transformation Cloudflare ; cet ancien HTML a depuis été retiré |
| Alignement PocketBase | Le compte Shopify doit avoir les mêmes accès et règles qu'un compte built-in | Compte `users`, token natif, règles et SDK testés ; confirmation utilisateur sur la boutique réelle à cette étape |
| OAuth natif | Faire passer le flux par `authWithOAuth2Code` ou `authWithOAuth2` | Fournisseur Go `shopify`, endpoint natif et redirection retenus ; une version intermédiaire a été confirmée en réel |
| Liaison d'identité | Comprendre et conserver l'identifiant Shopify | Liaison native `_externalAuths`, identifiants bruts dans une collection privée séparée |
| Noms | Les nouveaux comptes ne demandent pas de prénom/nom ; Contributor demande leur suppression | Requête et synchronisation limitées à l'ID et l'e-mail |
| Collection privée | Un simple préfixe `_` ne rangeait pas la collection dans System | Statut `System=true`, règles `nil`, renommage finalement approuvé en `_shopifyIdentities` |
| Configuration | Retirer le choix configurable de la collection d'auth | `users` fixé ; pas de `Config.AuthCollection` dans le code actuel |
| Session SDK | Contributor repère le bridge `/studio/session` comme reliquat | Supprimé, avec les cookies et les autres couches de démonstration |
| Nettoyage | Préparer à terme une librairie autonome | Nettoyage fait ; Contributor précise de ne pas préparer le packaging pour l'instant |
| Complexité frontend | Deux endpoints JSON et trop de passage de paramètres | Endpoints supprimés, adoption du parcours natif `listAuthMethods` + `authWithOAuth2Code` |
| Callback générique | `/studio/callback` ne convient pas comme nom de SDK générique | Le module n'impose ni ne sert de chemin ; README utilise `/auth/callback` à titre d'exemple |
| Test après nettoyage | Vérifier les changements contre la vraie boutique | Pas encore de nouveau test réel du contrat final ; Contributor demande un frontend minimal dans `pb_public` |
| Dernière discussion | À quoi sert `frontend.go`, et PocketBase ne sert-il pas déjà `pb_public` ? | Vérification faite pour cette passation ; réponse et prochaine action en section 6 |

### 2.2 Critères de réussite actuels

Le critère le plus important déjà formulé par Contributor reste l'équivalence avec l'authentification PocketBase native. Une page disant simplement "connecté" n'est pas une preuve suffisante.

La prochaine démonstration doit prouver :

- Connexion via le SDK officiel, sans sauvegarde manuelle d'un token issu d'une API de session maison.
- `pb.authStore` mis à jour par la réponse native de `authWithOAuth2Code`.
- Lecture API du propre compte après `authRefresh` et `getOne`, avec les règles du projet.
- Aucun accès indu aux données d'autres comptes ou à `_shopifyIdentities`.
- Retour silencieux si Shopify reconnaît une session ; proposition de login interactif en cas d'absence de session, sans boucle.
- Aucun token, code OAuth ou verifier affiché dans l'interface ou les logs.
- Données et liaisons existantes conservées, sans doublon et sans fusion par e-mail.

### 2.3 Différé ou non demandé

La déconnexion globale Shopify/studio, les vérifications périodiques de session, la popup OAuth, une interface MFA complète, une infrastructure multi-instance, un nouveau dépôt de librairie, une API publique de récupération de profil Shopify et la conservation de credentials externes ne sont pas les prochaines tâches autorisées par défaut.

## 3. Historique des décisions techniques

### 3.1 Customer Account API plutôt que partage de session Shopify

Problème : la session navigateur Shopify ne peut pas être utilisée comme credential PocketBase. Un e-mail ou ID inséré dans un lien n'est pas une preuve d'identité.

Solution retenue : utiliser les endpoints de découverte Shopify, OAuth2 Authorization Code avec PKCE et un ID token OIDC signé, puis lire le profil dans la Customer Account API. Cette voie correspond aux nouveaux comptes utilisés par Contributor.

L'enquête historique a aussi examiné :

- Le fournisseur OIDC générique de PocketBase. Un test existe encore. L'adaptation Shopify reste nécessaire pour la lecture GraphQL du profil, le sujet numérique observé et les règles d'identité du projet.
- L'app proxy Shopify et son `logged_in_customer_id` signé. Alternative non implémentée, avec vérification de signature et transfert interdomaines à concevoir ; pas d'avantage retenu ici.
- Les extensions de comptes clients et leurs session tokens. Elles supposent un contexte d'extension ; ce n'est pas le simple lien boutique vers studio demandé.
- Storefront API et `customerAccessTokenCreate`, orientés anciens comptes avec mot de passe. Non retenus pour ces nouveaux comptes.
- Multipass et fournisseur d'identité externe commun. Écartés dans l'enquête à cause de la contrainte non-Plus et du sens d'authentification recherché.
- App tierce faisant de Shopify un IdP. Dernier recours évoqué, aucun produit acheté ni testé.
- Admin API ou OAuth d'installation seuls. Ils ne prouvent pas l'identité du client dans le navigateur.

Ce sont des conclusions d'enquête archivées, pas une nouvelle certification de tous les produits et forfaits en octobre 2026. Le flux retenu a, lui, été validé historiquement sur une dev store non-Plus. Les paramètres actuels d'autorisation et `prompt=none` sont décrits dans la [référence Customer Account API](https://shopify.dev/docs/api/customer/2026-10#authorization).

### 3.2 Notre app Shopify plutôt que Headless obligatoire

Problème : Headless était indisponible sur la première boutique, qui affichait en réalité un forfait Trial.

Solution retenue : enregistrer le client via notre app Shopify et `[customer_authentication].redirect_uris`. Le scope d'installation de l'app actuelle est `customer_read_customers`. Le flux client demande les scopes OIDC/API `openid`, `email` et `customer-account-api:full`.

Conséquence : Headless n'est pas requis dans le montage actuel. Le client utilisé est public avec PKCE, sans secret. Le support exploratoire des clients confidential a été supprimé le 16 octobre 2026 du code et des tests.

Le scope d'installation, les scopes d'autorisation client et l'OAuth d'installation marchand sont trois choses différentes. Le callback client se configure dans `customer_authentication`, pas dans `[auth].redirect_urls`. Voir la [configuration d'app Shopify](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).

### 3.3 Compte `users` et pipeline PocketBase natif

Problème : le premier prototype utilisait une collection `studio_customers` et un parcours de session distinct. Cela ne satisfaisait pas l'exigence de fonctionnement identique aux comptes built-in.

Évolution retenue : intégrer d'abord les tokens et règles natifs, puis déplacer la connexion dans le véritable endpoint `auth-with-oauth2` via un fournisseur Go. Le SDK appelle désormais `authWithOAuth2Code`.

Conséquence : PocketBase garde la création du compte, la liaison `_externalAuths`, `CreateRule`, `AuthRule`, les hooks OAuth/création/auth, la MFA et l'enrichissement. Après connexion, il n'existe pas un autre type de token "Shopify" pour l'API du studio.

Fixer la collection à `users` est une décision de ce projet à la demande de Contributor. Ce n'est pas une limitation générale de PocketBase.

### 3.4 Redirection native plutôt que endpoints JSON de préparation

Problème : les étapes intermédiaires faisaient transiter les paramètres dans `/api/shopify/auth/start` et `/api/shopify/auth/complete`, avec un état de connexion côté serveur. Contributor jugeait cela trop complexe pour un flux censé être natif.

Solution retenue au commit `c9a9f0e` : utiliser les paramètres générés par `listAuthMethods()` et le callback frontend. Le frontend vérifie le state et le délai local ; le backend vérifie PKCE, callback et ID token dans l'échange natif.

Conséquences :

- Plus de cookies OAuth ou de session studio, ni de maps de transactions de login en production.
- Plus d'endpoint Shopify personnalisé.
- Plus de nécessité de revenir sur la même instance Go pour une transaction locale OAuth. Cela ne rend pas PocketBase multi-instance pour autant.
- Le contrôle du state est une responsabilité du frontend. Ne pas documenter un contrôle serveur qui n'existe plus.
- Le délai de cinq minutes est dans l'exemple frontend, pas dans une transaction serveur.

### 3.5 `authWithOAuth2Code` plutôt que popup `authWithOAuth2`

La navigation de premier niveau convient au SSO silencieux Shopify et évite d'introduire le callback et les mécanismes realtime de la popup native. Le fournisseur vérifie une URL de callback exacte, choisie par l'hôte.

La popup n'est pas déclarée impossible en principe. Elle n'est simplement ni activée, ni adaptée, ni testée dans l'état actuel. Appeler aveuglément `authWithOAuth2({provider: 'shopify'})` n'est pas un remplacement validé du code du README.

### 3.6 Nonce dérivé du challenge PKCE public

Problème : il faut vérifier le nonce OIDC tout en supprimant le stockage serveur des tentatives.

Solution retenue : `BuildAuthURL` prend le challenge S256 généré par PocketBase et l'utilise aussi comme nonce. Ce challenge est public, imprévisible par tentative et ne révèle pas le verifier. Au retour, le fournisseur recalcule S256 depuis le verifier soumis et compare ce résultat au nonce de l'ID token signé.

Le verifier n'est jamais ajouté comme nonce à l'URL Shopify. Conserver les tests de nonce substitué avec un PKCE pourtant correct.

### 3.7 Pas de fusion automatique par e-mail

PocketBase peut nativement sélectionner un compte existant par e-mail vérifié ou par l'authentification déjà présente dans l'en-tête `Authorization`. Le projet interdit ces rapprochements lorsqu'ils ne correspondent pas à une liaison Shopify exacte.

La politique retenue repose sur le couple issuer/sujet. Un conflit de compte ou d'e-mail renvoie `409`, sans fusion. Un compte natif ayant la même adresse ne devient pas automatiquement un compte Shopify.

Le frontend vide `pb.authStore` avant l'échange pour ne pas envoyer une session préalable comme candidat de liaison. Le backend garde sa vérification, car le frontend n'est pas une frontière de sécurité suffisante.

Il n'existe pas d'interface ou de procédure explicite de liaison volontaire de deux comptes. Ne pas supprimer cette protection pour contourner un `409`.

### 3.8 `_shopifyIdentities`, privée et système

Problème : les champs `shopify_customer_id`, `shopify_sub`, `shopify_issuer` placés dans `users` encombraient les comptes non Shopify et risquaient d'être exposés dans les profils. Contributor a demandé une collection séparée puis un vrai classement système dans l'admin.

Solutions successives : `_shopifyAccounts`, puis statut système, puis nom final `_shopifyIdentities` approuvé par Contributor.

Solution actuelle : collection base `System=true`, cinq règles API `nil`, relation obligatoire et à suppression en cascade vers `users`, identifiants bruts séparés. Le préfixe `_` seul n'assure aucune confidentialité. Un superuser conserve l'accès aux enregistrements système ; le but n'est pas de les lui cacher.

Le cœur actuel crée ou vérifie son schéma, mais ne rejoue plus les anciens nettoyages de `users` et ne transforme pas silencieusement n'importe quelle collection existante en collection système.

### 3.9 Suppression des prénoms et noms

La présence de ces champs dans le schéma Shopify ne garantit pas qu'ils soient renseignés. Le compte testé ne les avait pas, et Contributor a demandé de faire comme s'ils n'existaient pas.

Le code ne les demande plus à Shopify, ne les synchronise plus et n'ajoute aucun champ correspondant. La migration historique retire seulement les champs créés par l'ancien prototype lorsqu'elle est explicitement importée/exécutée dans ce contexte historique. Le cœur ne doit pas supprimer les champs de nom propres à un projet hôte.

### 3.10 Tokens Shopify absents des métadonnées

Les fournisseurs OAuth ordinaires peuvent renseigner `AuthUser.AccessToken`, `RefreshToken` et `RawUser`, repris dans les métadonnées de leur réponse native. Ici le fournisseur ne renseigne pas les deux tokens et renvoie un `RawUser` vide. Le SDK reçoit le token PocketBase utile à son API, pas les credentials Shopify.

Cette restriction ne signifie pas que les autres fournisseurs publient ces tokens à tous les visiteurs. Il s'agit de la réponse d'authentification remise au client concerné, pas de champs publics ajoutés à `users`. Le SDK ne conserve pas automatiquement toutes les métadonnées dans son auth store.

Conséquence assumée : l'identifiant Shopify reste disponible côté serveur, mais l'app ne peut pas faire indéfiniment des appels Customer Account API après le login avec un token qu'elle n'a pas conservé. Un nouvel appel de profil nécessiterait une nouvelle autorisation client ou une autre intégration autorisée. Un ID client n'est pas un bearer token.

Pour le type de client app utilisé ici, Shopify documente l'absence de refresh token. La réautorisation client et `authRefresh` PocketBase ne sont pas équivalents. Voir [l'obtention des tokens Shopify](https://shopify.dev/docs/api/customer/2026-10#obtain-access-token).

### 3.11 Restrictions d'origine et correction d'une explication antérieure

Le middleware attend exactement l'origine de `SHOPIFY_REDIRECT_URL` dans l'en-tête `Origin` du POST OAuth natif. Il ne compare pas cette origine au domaine de l'API PocketBase.

Un frontend et une API sur des origines différentes sont donc possibles si CORS et le proxy sont correctement configurés. Une affirmation antérieure de nécessité absolue d'une origine unique a été corrigée. Le même domaine reste le choix le plus simple pour le frontend minimal demandé.

Le navigateur fournit `Origin` automatiquement. Un client Node ou serveur doit l'ajouter pour reproduire ce contrat. Ce filtre est une défense côté navigateur, pas un credential secret ; un client serveur peut fabriquer cet en-tête. Le code garde PKCE et les validations cryptographiques.

Les tests locaux ne constituent pas une validation multi-origines dans un vrai navigateur.

### 3.12 Callback choisi par l'hôte

Contributor a contesté le nom `/studio/callback` pour un module générique. Le commit `75d85a7` précise que `RedirectURL` est l'URL complète choisie et servie par l'hôte. Aucun chemin de callback n'est fourni par le module.

Le README utilise `/auth/callback` comme exemple. Ce n'est pas une route actuellement implémentée. La configuration locale et le TOML Shopify restaient sur l'ancien chemin `/studio/callback` à l'inspection. Il faut coordonner les trois valeurs lors du prochain test, sans changer une seule extrémité isolément.

### 3.13 Librairie autonome différée

Le cœur ne dépend plus des anciennes pages, gateway et sessions de démonstration. Cela le rend plus facile à extraire, mais **le dépôt n'est pas encore une dépendance Go publiable**.

Le module s'appelle encore `studio-sso`, et le package est dans `internal/studio`. Go interdit son import par un module externe à son parent. Ne pas promettre un `go get` ou un import depuis un autre projet dans cet état. Contributor a explicitement demandé de ne pas préparer le packaging maintenant.

## 4. Travail présent dans les fichiers

### 4.1 Points d'entrée et responsabilités

| Fichier exact relatif à la racine | Fonctions / types principaux | État et responsabilité |
| --- | --- | --- |
| `main.go` | `main` | Crée PocketBase avec `DefaultDev: false`, lit la configuration, initialise le service dans `OnServe`, enregistre son middleware, démarre l'app |
| `internal/studio/studio.go` | `Service`, `New`, `Register`, `noStore`, `requireOrigin` | Configuration et assemblage du service ; `Register` n'ajoute aucune route |
| `internal/studio/provider.go` | `init`, `shopifyProvider`, `BuildAuthURL`, `FetchToken`, `FetchAuthUser`, `nativeOAuthMiddleware`, `identityID` | Fournisseur OAuth natif, PKCE/nonce, identité, middleware d'échange |
| `internal/studio/shopify.go` | `Config`, `checkURL`, `discover`, `shopTransport`, `readJSON`, `shopifySubjectKeySet.VerifySignature`, `fetchCustomer`, diagnostics | Discovery, transport, validation OIDC et profil Shopify |
| `internal/studio/store.go` | `ensureCollection`, `configureProvider`, `prepareOAuthRecord` | Configuration de `users`, hooks et politique de liaison |
| `internal/studio/accounts.go` | `ensureAccountsCollection`, `putAccount` | Schéma et écriture de `_shopifyIdentities` |
| `internal/studio/auth_response.go` | `captureAuthResponse`, `authResponseBuffer` | Retient la réponse native jusqu'à la fin des hooks pour éviter une fuite de JWT après rejet tardif |
| `internal/mockshop/mockshop.go` | `Mock`, `New`, `ServeHTTP`, autorisation et token simulés | Fournisseur HTTP pour tests, pas un service de production |

Repères de lignes observés à HEAD : `main.go:13`, `studio.go:27`, `provider.go:63`, `provider.go:77`, `provider.go:117`, `provider.go:135`, `shopify.go:89`, `shopify.go:135`, `shopify.go:161`, `store.go:33`, `store.go:80`, `accounts.go:16`, `auth_response.go:13`. Les numéros changeront après modification ; les noms de fonctions sont les repères durables.

### 4.2 Initialisation et configuration Go

`New` valide l'origine de boutique et le callback avant toute configuration. La boutique est une configuration de confiance de l'opérateur, pas un paramètre de requête client. Le callback fournit l'origine attendue du frontend.

Les URL sont HTTPS. L'exception HTTP doit être activée explicitement et ne vaut que pour `localhost` ou une adresse IP loopback. L'origine boutique ne doit avoir ni chemin ni slash final. Les URL configurées ne doivent pas contenir d'identifiants HTTP, de query string ou de fragment.

Le client HTTP a un timeout de dix secondes et refuse de suivre les redirections upstream. `shopTransport` clone les requêtes et renseigne l'origine du callback et le User-Agent prévu. Les lectures JSON sont limitées à 1 Mio.

La découverte lit `/.well-known/openid-configuration` et `/.well-known/customer-account-api`. Les endpoints annoncés sont validés comme URL admissibles ; ils ne sont pas obligés d'être sur le même nom d'hôte que la boutique, ce qui permet les domaines d'authentification Shopify. L'URL GraphQL utilisée vient de cette découverte, pas d'une constante de production `2026-10`.

Le démarrage nécessite donc le réseau vers Shopify. Un échec de discovery doit faire échouer le démarrage, pas basculer sur un faux fournisseur ou désactiver des contrôles.

### 4.3 Fournisseur OAuth et échange

L'enregistrement global `auth.Providers["shopify"]` crée un fournisseur sans capturer une boutique ou une instance PocketBase globale. Le middleware attache le service et le verifier à la requête concernée. Cette séparation est testée ; ne pas la remplacer par un singleton mutable contenant la dernière boutique configurée.

Le middleware :

1. Marque les réponses natives `auth-methods` en `no-store`.
2. Ne traite spécialement les POST `auth-with-oauth2` que pour `provider=shopify`.
3. Vérifie l'origine attendue.
4. Autorise la collection `users` par nom ou ID et refuse les autres collections, y compris `_superusers`.
5. Exige un verifier PKCE de 43 à 128 caractères dans l'alphabet autorisé.
6. Injecte le contexte requis par `FetchToken` et capture la réponse native jusqu'à la fin des hooks.

`FetchToken` impose le Client ID et l'URL de callback configurés. Il échange le code avec `oauth2.VerifierOption`, puis vérifie le jeton avec le JWKS et les algorithmes annoncés. Issuer, audience, expiration, nonce et `at_hash` si présent restent contrôlés. Le profil GraphQL est chargé seulement après la validation du jeton.

Pour un client public, PocketBase exige pourtant un champ de secret de configuration non vide. Le code utilise un marqueur public interne uniquement pour satisfaire cette validation. Ce marqueur n'est jamais envoyé à Shopify. L'échange utilise les paramètres, sans secret ni Basic Auth. Aucun mode confidential n'est conservé.

### 4.4 Bug réel du sujet OIDC numérique

Le premier test réel a échoué avec `Invalid Shopify ID token`, puis `verification_failed`, puis le diagnostic `claims_decode_failed`. Le type de `sub` observé était numérique alors que `go-oidc` attend une chaîne.

`shopifySubjectKeySet` reçoit la validation de signature de la bibliothèque et demande d'abord la vérification du JWT original. Il normalise ensuite seulement un `sub` entier décimal positif pour le décodeur de claims. Aucune conversion en `float64` n'intervient. Les valeurs négatives, fractionnaires, en notation exponentielle ou de type inattendu sont refusées. Un sujet textuel reste inchangé.

La vérification de signature porte donc sur les octets signés d'origine, pas sur un JWT réécrit. Ne pas désactiver les contrôles OIDC ou décoder naïvement le payload pour authentifier le client. Cette adaptation dépend du comportement de `go-oidc v3.21.0` ; ses tests doivent accompagner toute mise à jour.

Les diagnostics actuels renvoient des catégories fixes et des types JSON de champs connus, jamais les valeurs des claims, le JWT, le token ou les réponses upstream brutes. La lecture diagnostique d'un payload non vérifié ne sert pas à authentifier.

### 4.5 Profil et stockage d'identité

La requête de profil ne sélectionne que `customer.id` et `customer.emailAddress.emailAddress`. L'en-tête Customer Account API contient le token brut, sans préfixe `Bearer`. Le test simulé vérifie ce contrat.

Une erreur GraphQL, un ID client absent/incompatible ou un e-mail absent bloque la connexion. Le fournisseur ne marque l'e-mail comme vérifié que si le claim `email_verified` validé est vrai et si l'adresse du claim correspond à l'adresse GraphQL.

`identityID` calcule le SHA-256 de `issuer + séparateur NUL + subject`, en hexadécimal. Ce hash est le `providerId` utilisé dans `_externalAuths`. Il namespace les identités par issuer et évite d'arrondir les sujets numériques.

`_shopifyIdentities` contient :

| Champ | Type / contrainte | Usage |
| --- | --- | --- |
| `user` | Relation requise, un seul `users`, suppression en cascade | Compte PocketBase associé |
| `issuer` | Texte requis | Émetteur OIDC de l'identité |
| `subject` | Texte requis | Sujet Shopify exact, même si initialement numérique |
| `customerId` | Texte requis | ID GraphQL Shopify du client |

Deux index uniques portent respectivement sur `issuer, subject` et `user, issuer`. Les cinq règles `List`, `View`, `Create`, `Update`, `Delete` sont `nil`, donc verrouillées pour les comptes ordinaires. La collection est système. Les tests couvrent aussi l'absence de fuite via expansion et realtime.

Le compte reste dans `users`, la liaison OAuth native dans `_externalAuths`, les valeurs brutes propres au fournisseur dans `_shopifyIdentities`. Aucune de ces trois responsabilités ne doit être remplacée par un simple e-mail.

### 4.6 Hooks, création et conservation des comptes

`configureProvider` ajoute ou remplace seulement le fournisseur `shopify`, active OAuth2 et conserve les autres fournisseurs, règles, durées de tokens, mot de passe, OTP et MFA.

`prepareOAuthRecord`, hook de priorité `-1000`, exige un profil Shopify déjà vérifié. Il recherche l'identité privée et la liaison native exacte. Si une ancienne interruption a laissé `_externalAuths` sans ligne privée, il peut récupérer le compte depuis cette liaison exacte. Il ne récupère pas un compte par simple correspondance d'e-mail.

Pour un nouveau compte, le hook impose l'e-mail de profil dans `CreateData` et retire les valeurs client `verified`, `password`, `passwordConfirm`. Pour un compte existant, il synchronise l'e-mail et sa vérification avec un `Save` natif. Les champs de profil propres à l'hôte sont conservés.

La synchronisation via `Save` déclenche les hooks de modèle. Elle ne simule pas une requête publique `PATCH` et ne promet donc pas de déclencher les hooks de requête d'une telle API. La création initiale et l'authentification passent bien par le parcours natif et ses hooks.

Le hook `OnRecordAuthRequest` écrit la ligne privée lors du premier facteur OAuth validé. Si la MFA est active, PocketBase peut déjà avoir créé le compte et ses liaisons avant de demander le second facteur. Cela ne signifie pas qu'une session finale a été délivrée.

### 4.7 Pourquoi conserver `auth_response.go`

Un hook peut refuser après avoir appelé `e.Next()`. Sans précaution, une réponse native contenant déjà un JWT pourrait avoir été écrite avant ce refus.

`captureAuthResponse` capture le statut, les en-têtes et le corps, puis ne transmet la réponse de succès qu'après le retour de toute la chaîne. Il ne fabrique aucun token et ne remplace pas la logique d'authentification PocketBase. Les erreurs natives, challenges MFA et enrichissements restent natifs.

Ce fichier n'est pas un reliquat de session de démonstration. Ne pas le supprimer dans un nettoyage sans conserver une protection équivalente et ses tests de refus tardif.

### 4.8 Fichiers de test et ce qu'ils valident

| Fichier | Contenu principal |
| --- | --- |
| `internal/studio/studio_test.go` | Fixture HTTP PocketBase/Shopify, bases temporaires, parcours, sécurité OIDC, sujet numérique, diagnostics, profils, expirations, anciennes routes absentes |
| `internal/studio/oauth_test.go` | Collection cible, hooks OAuth/création, PKCE, nonce, origine, rejeu, conservation de liaison, conflits, configuration des autres fournisseurs, `CreateData` falsifié |
| `internal/studio/pocketbase_test.go` | Comparaison Shopify/password sur les règles API, propriétaire de documents, auth rule, hooks, MFA, création, enrichissement et token natif modifié |
| `internal/studio/realtime_test.go` | Connexion SSE locale et filtrage d'événements selon les mêmes règles de propriétaire |
| `internal/studio/accounts_test.go` | `users` requis, schéma hôte préservé, collection privée, protections système, récupération par liaison exacte, cascade |
| `internal/studio/sdk_test.go` | Démarre les fixtures puis lance les deux scripts Node avec le SDK officiel si le flag d'intégration est activé |
| `integration/oauth.mjs` | Extrait les deux fonctions balisées du README et les exécute telles quelles avec le SDK officiel et de vrais serveurs HTTP locaux |
| `integration/sdk.mjs` | `authStore`, `authRefresh`, lecture et CRUD comparés pour Shopify et compte password |
| `migrations/1792095899_remove_profile_names_test.go` | Nettoyage historique unique, préservation, refus sans changement partiel, installation neuve |
| `migrations/1792097400_rename_shopify_identities_test.go` | Renommage historique avec conservation et refus de schémas/conflits incompatibles |

Les méthodes de fixture nommées `start` et `callback` n'indiquent pas la présence de routes personnalisées en production. Elles émulent le navigateur et son stockage temporaire dans les tests Go. La map `fixture.flows` est un stockage de test, pas une transaction serveur de production.

Le mock utilise des clés RSA, un JWKS, des JWT signés, PKCE, des codes à usage unique et un endpoint GraphQL. La navigation et Web Storage sont simulés dans Node. Ce ne sont pas des tests dans Chrome/Safari ni contre les serveurs Shopify réels.

Le test `integration/oauth.mjs` vérifie notamment le refus local des callbacks avec mauvais state, stockage absent/corrompu, délai dépassé ou code absent avant tout échange ; le nettoyage de l'adresse ; l'absence de cookie PocketBase ; la mise à jour de l'auth store et son `onChange` ; le rejeu local et upstream ; la persistance par `LocalAuthStore`, le refresh et la déconnexion locale.

### 4.9 Migrations historiques

Deux migrations Go restent dans le dépôt et sont testées :

- `migrations/1792095899_remove_profile_names.go` : retire les anciens champs `first_name` et `last_name` du prototype avec validation, sans réécrire les règles de l'hôte. Le down refuse de prétendre restaurer les données supprimées et exige une sauvegarde. La variable historique de choix de collection existe seulement dans ce nettoyage.
- `migrations/1792097400_rename_shopify_identities.go` : renomme `_shopifyAccounts` en `_shopifyIdentities`, conserve l'identité de collection et utilise exceptionnellement `SaveNoValidate` après validation parce que le nom d'une collection système est protégé. Refuse de fusionner deux collections existantes.

`main.go` et `internal/studio` **n'importent pas** le package local `migrations`. Ces nettoyages ne s'exécutent donc pas automatiquement dans le programme actuel. Le cœur actuel ne contient pas l'ancien `SaveNoValidate` de promotion ou de migration de schéma.

Ne pas ajouter un import à effets de bord de ces migrations pour résoudre un problème d'installation dans un autre projet. Elles appartiennent à l'historique de cette base de démonstration.

### 4.10 Suppressions confirmées par Git

Le commit `9d04979` supprime :

```text
D:/Workspaces/shopify-pocketbase-sso/cmd/gateway/main.go
D:/Workspaces/shopify-pocketbase-sso/cmd/gateway/main_test.go
D:/Workspaces/shopify-pocketbase-sso/cmd/mockshop/main.go
D:/Workspaces/shopify-pocketbase-sso/internal/studio/web/LICENSE.pocketbase.md
D:/Workspaces/shopify-pocketbase-sso/internal/studio/web/callback.js
D:/Workspaces/shopify-pocketbase-sso/internal/studio/web/client.js
D:/Workspaces/shopify-pocketbase-sso/internal/studio/web/pocketbase.es.mjs
D:/Workspaces/shopify-pocketbase-sso/shopify.app.example.toml
```

Le commit suivant `c9a9f0e` retire le contrat des endpoints JSON de préparation/complétion introduit pendant le nettoyage. Les tests actuels attendent leur absence. Les binaires anciens dans `bin/` ne signifient pas que leurs sources ou routes sont encore actives.

### 4.11 Validations exécutées pour cette passation

Exécution fraîche le 16 octobre 2026 sur l'arbre correspondant à `75d85a7`, avant ajout de ce document :

```powershell
$env:RUN_JS_SDK_TEST = '1'
go test ./... -count=1 -cover
go vet ./...
go build ./...
node --check integration/oauth.mjs
node --check integration/sdk.mjs
git diff --check
```

Résultats vérifiés :

| Contrôle | Résultat |
| --- | --- |
| `go test ./... -count=1 -cover` avec les intégrations SDK | Succès, code de sortie 0 |
| `internal/studio` | 49 fonctions/groupes de test, couverture 86,8 %, environ 15,6 s lors de cette exécution |
| `migrations` | 5 fonctions/groupes de test, couverture 81,0 % |
| Package racine | Pas de test de l'hôte ; couverture 0,0 % |
| `internal/mockshop` | Pas de test propre à ce package ; couverture directe affichée 0,0 %, bien qu'il soit utilisé par les tests du cœur |
| `go vet ./...` | Succès |
| `go build ./...` | Succès |
| Syntaxe des deux scripts Node | Succès |
| `git diff --check` | Succès avant rédaction du document |

Les 49 groupes incluent les deux tests SDK. Sans `RUN_JS_SDK_TEST`, ces deux tests sont explicitement ignorés. Ne pas annoncer une validation SDK si ce flag n'est pas activé.

Le `npm ci` n'a pas été relancé pour la passation, car le SDK fixé et installé était déjà présent ; les intégrations ont effectivement passé. La commande de réinstallation reproductible figure en section 8.

Les archives rapportent aussi un ancien `govulncheck` sans vulnérabilité atteignable et un `npm audit` sans vulnérabilité. Ces audits n'ont pas été relancés pour ce document. Ne pas présenter ce résultat historique comme un audit à jour ni comme une certification de sécurité. Les tests `-race` n'ont pas été exécutés dans cet environnement `CGO_ENABLED=0`.

### 4.12 Tests réels historiques et frontière de validation

Les confirmations visibles de Contributor attestent successivement :

- Une arrivée sur son profil après correction du sujet Shopify numérique.
- Le message `API PocketBase : accès confirmé avec le SDK officiel.` après l'alignement API.
- Une nouvelle ouverture explicite de l'ancien `/studio/auth` et le même message après passage à une première version du fournisseur OAuth natif.

Les archives rapportent aussi l'installation de l'app sur la dev store, la découverte publique, le retour `login_required` anonyme, la lecture réelle du profil et la création de la liaison native sans doublon.

**Ces confirmations précèdent le dernier remplacement par le parcours `listAuthMethods` sans cookie/endpoint de préparation. Ce contrat final n'a pas encore été revalidé de bout en bout sur la vraie boutique avec un frontend hôte.** C'est précisément la raison de la demande de frontend dans `pb_public`.

## 5. État actuel du projet

### 5.1 Fonctionnel et vérifié dans le code / les tests

- Fournisseur Shopify enregistré dans PocketBase, échange réel de code dans le pipeline natif.
- Validation OIDC, PKCE, nonce et profil, avec support du sujet numérique observé.
- Comptes `users`, liaison native et identité privée cohérentes.
- Règles API, CRUD, refresh, hooks et realtime local comparés au compte built-in.
- MFA appliquée par PocketBase ; aucun token final délivré avant second facteur.
- Pas de fusion automatique, pas de token externe dans les métadonnées, pas de bridge de session.
- Schéma et politiques hôte conservés, autres fournisseurs préservés.
- Tests d'intégration SDK officiels passants.

### 5.2 Non livré ou partiel

- Aucun frontend minimal dans l'arbre actuel, malgré une tentative rapportée dans le contexte conservé.
- Aucun service statique de `pb_public` dans l'hôte actuel.
- Test réel du contrat final après nettoyage encore à faire.
- Callback neutre documenté mais configuration locale/app encore sur l'ancien chemin.
- Pas de bibliothèque importable depuis un autre module Go ; packaging différé par Contributor.
- Pas d'interface MFA ni d'envoi réel d'alertes validé.
- Pas de méthode publique de relecture continue du profil Shopify.
- Pas de synchronisation de déconnexion Shopify/PocketBase.

### 5.3 État des processus vérifié

Lors de l'inspection :

- Zéro processus dont l'exécutable se trouve dans le répertoire projet, et zéro processus `cloudflared.exe` trouvé.
- Aucun listener sur les ports locaux 8090, 8091 ou 8092.
- `http://127.0.0.1:8090/api/health` inaccessible, connexion impossible.
- Aucun backend ou tunnel n'a été démarré pendant la passation.

Les anciennes mentions "processus laissés actifs" dans les archives ne sont plus vraies. Une nouvelle session d'agent doit vérifier les processus et les ports avant tout démarrage ; les PID historiques ne sont pas des cibles fiables.

### 5.4 Base locale vérifiée en lecture seule

Chemin : `D:/Workspaces/shopify-pocketbase-sso/pb_data/data.db`.

Contrôles réalisés avec `node:sqlite`, `DatabaseSync` en lecture seule. Aucune valeur de compte, adresse, identifiant brut ou credential n'a été affiché.

| Collection | État actuel | Lignes |
| --- | --- | --- |
| `users` | Auth, non système | 1 |
| `_shopifyIdentities` | Base, système | 1 |
| `_externalAuths` | Base, système | 1 |
| `studio_customers` | Absente | Sans objet |
| `_shopifyAccounts` | Absente | Sans objet |

Les lignes complètes des trois collections présentes ont été comparées en mémoire à `pb_backups/before-public-frontend-20261016.db`. Elles sont identiques. `PRAGMA quick_check` retourne `ok`.

Les champs de `users` sont actuellement les champs natifs, plus `name`, `avatar`, `created`, `updated`. Il n'y a pas de `shopify_customer_id`, `shopify_sub`, `shopify_issuer`, `first_name` ou `last_name`. La présence du champ hôte `name` ne signifie pas qu'il faut le synchroniser depuis Shopify.

Les règles de `users` sont filtrées pour liste, lecture, mise à jour et suppression ; la création est publique. Les cinq règles de `_shopifyIdentities` sont verrouillées. Le cœur n'impose pas les expressions des règles de `users`.

Les archives mentionnent que les anciens comptes `studio_customers` étaient conservés à une étape intermédiaire. Cela ne décrit plus cette base : la collection est absente maintenant. Ne pas annoncer leur présence ni une migration automatique de ces comptes.

### 5.5 Sauvegardes et binaires à préserver

Sauvegardes présentes, toutes sous `D:/Workspaces/shopify-pocketbase-sso/pb_backups/` :

```text
before-native-auth-20261015.db
before-native-oauth2-20261015.db
before-private-accounts-20261015.db
before-system-email-only-20261015.db
before-identities-users-only-20261015.db
before-demo-stop-20261015.db
before-native-redirect-20261015.db
before-public-frontend-20261016.db
```

Certaines ont des fichiers compagnons `-wal` et `-shm`. Ne pas les supprimer au titre du ménage. Avant toute opération de restauration ou copie d'une base active, prendre une sauvegarde SQLite cohérente, pas une copie isolée hasardeuse d'un fichier en mode WAL.

Binaires présents dans `bin/`, dont : `pocketbase-native-redirect.exe`, `pocketbase-browser-test.exe`, `pocketbase-auth.exe`, plusieurs `studio-*.exe`, `gateway*.exe`, `mockshop.exe`, `cloudflared.exe`.

`pocketbase-browser-test.exe` date de l'étape rapportée du frontend. Son existence ne permet pas de récupérer les fichiers statiques disparus et ne garantit pas une correspondance à HEAD. Pour une validation de code, reconstruire à partir des sources choisies, ne pas utiliser un ancien exécutable par commodité.

### 5.6 État Git vérifié

Dépôt principal :

- Racine : `D:/Workspaces/shopify-pocketbase-sso`.
- Branche : `main`.
- HEAD complet : `75d85a79b31d981f69c6226175519007beb7d029`.
- Dernier commit : `Clarify OAuth callback ownership`, 15 octobre 2026 à 23:57:47 +02:00.
- Remote `origin` configuré sur le dépôt GitHub `Sov3rain/shopify-pocketbase-sso`.
- Aucun fetch, push, commit, reset, checkout ou stash n'a été effectué pendant la passation. L'état distant n'a pas été vérifié.
- Avant rédaction : aucun changement suivi, aucun changement indexé ; seul `?? shopify-app/`.
- Après rédaction, le changement attendu est l'ajout non suivi de `CODEX_HANDOFF.md`, en plus de `shopify-app/`.

Historique local, du plus ancien au plus récent :

| Commit | Sujet |
| --- | --- |
| `02f3739` | initial commit |
| `d43f20c` | Align Shopify SSO with PocketBase auth |
| `cafaf6b` | Use PocketBase's native Shopify OAuth flow |
| `e23b923` | Store Shopify identities in a private collection |
| `7db38a9` | Make Shopify accounts system-only and email-only |
| `891b707` | Rename Shopify accounts to identities |
| `9d04979` | Replace demo UI with OAuth JSON endpoints |
| `c9a9f0e` | Use native PocketBase OAuth redirect flow |
| `75d85a7` | Clarify OAuth callback ownership |

Le projet `shopify-app/studio-sso` est un **dépôt Git imbriqué distinct**, branche `main`, sans commit. Son `git status` liste des fichiers non suivis. La racine principale le présente comme un répertoire non suivi, pas comme un sous-module configuré. Il n'est pas sauvegardé par les commits actuels du dépôt principal. Ne pas l'effacer ni l'ajouter aveuglément à Git.

### 5.7 Limitations et risques connus

- Déconnexion locale seulement. `pb.authStore.clear()` ne déconnecte pas Shopify, ne révoque pas les copies d'un token et n'empêche pas une reconnexion silencieuse.
- `authRefresh` renouvelle PocketBase sans revalider la session Shopify. Aucune durée maximale depuis la dernière preuve Shopify n'est imposée par ce module.
- Un changement de compte Shopify n'invalide pas automatiquement un studio déjà connecté.
- Une seule configuration de callback et d'origine par instance du service. Pas de sélection arbitraire de boutique/callback depuis le navigateur.
- Une seule entrée `shopify.oauth` dans le `sessionStorage` de l'onglet. Deux tentatives simultanées dans le même onglet se remplacent. Les cas d'onglets dupliqués et comportements de vrais navigateurs restent à vérifier si nécessaires.
- `prompt=none` peut demander une interaction ou un consentement. Ne jamais promettre une absence absolue de redirections ou de formulaire dans tous les cas.
- Le secret PKCE temporaire et le token PocketBase sont accessibles au JavaScript frontend. La prévention XSS est une obligation de l'hôte.
- Pas de validation actuelle Chrome/Safari/mobile du dernier parcours. Les outils navigateur/computer-use n'étaient pas disponibles lors de la tentative rapportée.
- Les anciens Quick Tunnels Cloudflare ne sont pas une validation du realtime distant. L'archive signale l'absence de prise en charge SSE de cette voie ; le test SSE actuel est local.
- Les permissions et l'accès aux données client protégées sur une boutique commerciale cible restent à contrôler. Une réussite sur dev store Basic ne certifie pas toutes les configurations commerciales.
- `ensureAccountsCollection` refuse un schéma existant incompatible. Ce refus est voulu ; ne pas contourner la validation pour "faire démarrer" l'app.
- Le buffer de réponse évite une fuite de token après un rejet de hook. Il ne promet pas une annulation rétroactive de tout effet secondaire d'un hook ou d'un `Save` déjà exécuté.
- La racine n'a actuellement aucun test de service statique ou de frontend. La couverture du cœur ne couvre pas une UI absente.

## 6. Travail interrompu au moment de la passation

### 6.1 Demande active avant le document

Après le nettoyage, Contributor a demandé les éléments nécessaires pour un vrai test. Il a choisi de créer un frontend minimal dans `pb_public`, servi par PocketBase comme dans la documentation officielle. Il a ensuite questionné la nécessité de `frontend.go`, puis fourni la discussion PocketBase 132 sur le service automatique de ce dossier.

La demande de passation a interrompu cette clarification. Elle n'autorise aucune modification source pendant la rédaction de ce document.

### 6.2 Tentative de frontend rapportée par le contexte conservé

Le contexte de session conservé décrit une tentative avec les éléments suivants. **Ils ne sont pas présents dans l'arbre actuel, ne figurent dans aucun des commits listés et ne doivent pas être annoncés comme livrés.**

| Chemin rapporté | Rôle envisagé / rapporté | Constat actuel |
| --- | --- | --- |
| `frontend.go` | Helper de service de fichiers via `apis.Static`, en-têtes de sécurité et protection des préfixes réservés | Absent |
| `frontend_test.go` | Tests HTTP du service statique/callback et parité avec l'exemple README | Absent |
| `pb_public/index.html` | UI française minimale de validation | Absent |
| `pb_public/app.js` | SDK officiel, boutons silencieux/interactif, callback, refresh, lecture propre compte, logout local | Absent |
| `pb_public/style.css` | Style local minimal | Absent |
| `pb_public/vendor/pocketbase.es.mjs` | Copie non modifiée du SDK officiel fixé | Absent |
| `pb_public/vendor/LICENSE.pocketbase.md` | Licence du SDK copié | Absent |
| `main.go` | Appel au helper de frontend après enregistrement du service Shopify | Aucun appel présent |
| `README.md`, `TESTS.md` | Documentation du frontend hôte | État actuel revenu au contrat sans frontend servi |

Le contexte rapportait un frontend hôte, hors de `internal/studio`, avec deux boutons de login, vérification API et logout local, sans affichage de token. Il réutilisait les fonctions du README et ne réintroduisait pas les anciennes APIs de session.

Il rapportait aussi des tests HTTP et Node locaux de cette tentative, la compilation de `bin/pocketbase-browser-test.exe` et une sauvegarde avant démarrage. Ces deux artefacts existent encore. Les tests de frontend rapportés ne sont pas reproductibles dans l'arbre actuel, leurs sources étant absentes. Ne pas les additionner aux 49 groupes du cœur comme s'ils avaient été relancés pour la passation.

Aucun test de navigateur réel et aucune nouvelle connexion Shopify du dernier parcours n'avait été réalisé lors de cette tentative. L'indisponibilité du navigateur pilotable était une limite de l'environnement de test, pas une erreur prouvée du flux Shopify.

Le runtime rapporté comme démarré alors ne tourne plus maintenant. La base a néanmoins été vérifiée cohérente et inchangée depuis la sauvegarde correspondante.

### 6.3 Réponse technique à la question `pb_public`

Vérifications effectuées :

1. Lecture de la [discussion 132](https://github.com/pocketbase/pocketbase/discussions/132). Elle parle du binaire officiel ; un commentaire précise le cas de l'utilisation comme framework.
2. Lecture du code de la version réellement fixée `v0.40.5`, et pas seulement du snippet de 2022.
3. `examples/base/main.go` enregistre explicitement dans `OnServe` une route GET wildcard utilisant `apis.Static` avec un `os.DirFS` et un flag de fallback `index.html`.
4. `cmd/serve.go` appelle `apis.Serve`, et le routeur standard sert l'admin, mais le projet n'importe pas le programme `examples/base/main.go`.
5. Notre `main.go` ne déclare pas cette route statique.

Conclusion à transmettre à Contributor : oui pour l'exécutable PocketBase distribué ; non automatiquement pour notre programme Go personnalisé. Le code statique est une responsabilité de l'hôte. Si l'on veut éviter un fichier `frontend.go`, quelques lignes peuvent rester dans `OnServe` de `main.go`. Aucun besoin de modifier le fournisseur Shopify pour cela.

Références de version : [programme officiel de base v0.40.5](https://github.com/pocketbase/pocketbase/blob/v0.40.5/examples/base/main.go), [handler `apis.Static` v0.40.5](https://github.com/pocketbase/pocketbase/blob/v0.40.5/apis/base.go), [service API v0.40.5](https://github.com/pocketbase/pocketbase/blob/v0.40.5/apis/serve.go).

Fichiers locaux consultables sans réseau :

```text
C:/Users/USER/go/pkg/mod/github.com/pocketbase/pocketbase@v0.40.5/examples/base/main.go
C:/Users/USER/go/pkg/mod/github.com/pocketbase/pocketbase@v0.40.5/apis/base.go
C:/Users/USER/go/pkg/mod/github.com/pocketbase/pocketbase@v0.40.5/apis/serve.go
C:/Users/USER/go/pkg/mod/github.com/pocketbase/pocketbase@v0.40.5/cmd/serve.go
```

Ne pas copier l'ancien `OnBeforeServe` / `StaticDirectoryHandler` de la discussion de 2022. Dans cette version, utiliser `OnServe`, `apis.Static` et le paramètre wildcard attendu `{path...}`.

Le fallback vers `index.html` permet de servir un callback de type SPA sur `/auth/callback`, mais peut aussi transformer une URL API inconnue en HTML si le catch-all est trop large. Les préfixes natifs API/admin et les anciennes routes supprimées doivent rester testés. `apis.Static` désactive les logs d'activité des succès statiques ; les erreurs restent journalisées. Les logs de proxy doivent aussi protéger les query strings du callback.

### 6.4 Prochaine action concrète

À la reprise de développement, commencer par lire `main.go`, la section callback du README et le handler statique du programme officiel fixé. Répondre à Contributor sur la distinction binaire/framework et expliquer que `frontend.go` est facultatif, puis rétablir un frontend hôte minimal conformément à sa demande, en tenant compte de l'absence actuelle des fichiers.

Ne pas commencer par lancer `pocketbase-browser-test.exe`, restaurer une base, réintroduire la gateway, changer le fournisseur ou publier une configuration Shopify. Le blocage immédiat est l'absence du frontend et de sa route dans les sources, pas un bug d'authentification encore identifié.

## 7. Plan de continuation priorisé

### Priorité 0 : confirmer l'état à l'ouverture de T3 Code

Objectif : éviter de repartir d'une hypothèse devenue fausse entre la passation et la reprise.

Fichiers : `CODEX_HANDOFF.md`, `README.md`, `TESTS.md`, `main.go`, `shopify-app/studio-sso/AGENTS.md` si travail dans l'app.

Actions : relever branche, HEAD et `git status`; vérifier `pb_public`, `frontend.go`, `frontend_test.go`; vérifier listeners et processus ; constater sans modifier les fichiers locaux de configuration.

Vigilance : ne pas écraser des changements de Contributor ou d'un autre agent. Le répertoire Shopify imbriqué n'est pas suivi par le dépôt principal. Ne pas utiliser les anciens PID ou considérer l'ancien tunnel comme actif.

Validation : état initial écrit en quelques lignes ; aucun changement source tant que le périmètre de reprise n'est pas établi.

### Priorité 1 : finir la clarification et servir le frontend hôte simplement

Objectif : répondre à la dernière question puis fournir la page de test demandée, hors du cœur OAuth.

Fichiers à créer/modifier lors de la future reprise, pas pendant cette passation : `main.go`, `pb_public/index.html`, `pb_public/app.js`, éventuellement un CSS et un dossier vendor ; tests hôte si nécessaires. Un `frontend.go` séparé reste facultatif.

Instructions :

- Enregistrer le handler statique fourni par PocketBase dans l'hôte, avec une résolution de dossier explicite.
- Ne pas créer une abstraction ou un plugin générique pour quelques fichiers de test.
- Choisir un callback frontend servi, idéalement le chemin neutre déjà illustré `/auth/callback`, mais ne pas le considérer enregistré dans Shopify avant coordination.
- Réutiliser les deux fonctions README ou garantir leur parité par test. Ne pas écrire un second protocole OAuth.
- Servir le SDK officiel fixé localement si l'on veut une CSP sans CDN, avec sa licence et une vérification de copie. `integration/node_modules` n'est pas un répertoire à exposer au navigateur.
- Appliquer `Cache-Control: no-store` et `Referrer-Policy: no-referrer` au callback. Ne charger ni analytics ni scripts tiers ; préférer des scripts locaux avec une CSP adaptée.
- Préserver les routes PocketBase et éviter le fallback HTML sur les préfixes API/admin ou les endpoints supprimés.

Vigilance : le chemin `./pb_public` dépend du répertoire courant. Le binaire officiel a sa propre logique de résolution à côté de l'exécutable ; le programme personnalisé ne l'hérite pas. Avec un binaire dans `bin/`, ne pas déplacer involontairement les chemins de données ou de frontend.

Validation : tests HTTP de `/`, JS, SDK et callback ; 404 pour les anciennes APIs personnalisées ; API native et admin non remplacés par du HTML ; en-têtes du callback ; aucune query OAuth réinjectée dans le HTML ; aucun cookie de session ajouté.

### Priorité 2 : valider le comportement de la page locale

Objectif : prouver l'auth store et l'API, pas seulement le rendu d'une page.

Fichiers : frontend hôte, tests hôte, README, `integration/oauth.mjs`, éventuellement `internal/studio/sdk_test.go` sans changer le contrat.

Instructions :

- Bouton connexion interactive et moyen explicite de tester `prompt=none`.
- Callback traité une seule fois, adresse nettoyée avant l'échange, stockage temporaire consommé même en cas d'erreur.
- Traiter les erreurs silencieuses attendues en proposant le bouton, sans relancer depuis le callback.
- Après succès, utiliser la même instance SDK et vérifier `authRefresh` puis `getOne` du propre compte. Ne pas appeler `authStore.save` pour injecter une session venue d'un endpoint maison.
- Bouton logout limité à `pb.authStore.clear()` et au nettoyage des paramètres temporaires ; expliquer sa portée locale.
- N'afficher que les informations utiles, jamais token/code/verifier/ID Shopify brut. Rendre les chaînes en texte, pas en HTML arbitraire.
- Signaler proprement la MFA si elle est activée sans prétendre fournir son interface complète.

Vigilance : `pb.authStore.isValid` ne constitue qu'un contrôle local du token. Une lecture native réussie confirme la session côté serveur. Éviter de journaliser un objet d'erreur SDK complet, qui peut contenir la requête et ses credentials.

Validation : suite Go avec SDK activé, tests frontend utilisant le SDK officiel, puis inspection dans un navigateur disponible. Une simulation Node du DOM ne suffit pas à annoncer Chrome/Safari validés.

### Priorité 3 : préparer les trois URL et un accès HTTPS sûr

Objectif : permettre le vrai test sans casser l'app ou exposer inutilement l'admin.

Fichiers : `config.local.ps1`, `shopify-app/studio-sso/shopify.app.toml`, constante de callback du frontend, documentation de test.

Instructions :

- Choisir l'origine HTTPS de test disponible et le callback complet.
- Aligner exactement frontend, `SHOPIFY_REDIRECT_URL` et `[customer_authentication].redirect_uris`.
- Si un nouveau tunnel change l'origine, traiter aussi le contrôle `Origin`, le redémarrage du backend et la configuration publiée Shopify.
- Valider le TOML avec Shopify CLI avant publication.
- Les changements externes doivent rester limités à l'app/dev store de test autorisée. Ne pas déployer vers une boutique commerciale, changer de forfait ou accepter des frais implicitement.
- Ne pas réactiver automatiquement l'ancienne gateway supprimée. Choisir avec Contributor une exposition minimale ou un reverse proxy de test qui ne publie pas inutilement `/_/`, les superusers ou les paramètres.

Vigilance : une CLI authentifiée sur le poste ne garantit pas que le prochain agent ait une session encore valide. Les codes e-mail sont saisis par Contributor et ne doivent pas être demandés dans le chat. Le tunnel est une dépendance de test, pas l'architecture de la future librairie.

Validation : discovery Shopify, health local, callback HTML sur la bonne origine, autorisation vers le bon client, validation de configuration CLI ; aucun secret dans les commandes/logs. Faire une sauvegarde cohérente avant tout redémarrage avec évolution de schéma.

### Priorité 4 : effectuer le test réel du contrat final

Objectif : lever la limite principale encore ouverte.

Scénarios :

1. Même profil navigateur que la boutique, client Shopify connecté, studio sans session locale : tentative silencieuse et accès API confirmé, sans nouveau code e-mail si Shopify autorise le SSO.
2. Fenêtre privée, studio direct : retour silencieux sans session, affichage du bouton, login interactif et retour connecté.
3. Rafraîchissement de page et nouvelle instance du SDK : auth store restauré, API toujours conforme aux règles.
4. Déconnexion locale : API non utilisable par cette instance, Shopify encore éventuellement connecté, nouvelle tentative silencieuse conforme à cette indépendance.
5. Callback rejoué ou mauvais state : pas d'échange ni fuite de token.
6. Pas de nouvelle ligne `users`, identité privée et liaison exactes conservées pour le client déjà connu.

Fichiers : noter le résultat dans `TESTS.md`, et les prérequis réellement utilisés dans README, sans valeurs de secrets ni données personnelles.

Vigilance : distinguer les résultats utilisateur, les contrôles HTTP anonymes et les tests simulés. La connexion à l'admin marchand ou le mot de passe de protection d'une dev store ne constitue pas une connexion de client Shopify.

Validation : confirmation de Contributor sur la page et vérification serveur en lecture seule par compteurs/indicateurs, sans extraire de données personnelles. Documenter date, commit et origine de test de façon non sensible.

### Priorité 5 : terminer le ménage documentaire après validation

Objectif : que les commandes du README décrivent ce qui est réellement livré.

Fichiers : `README.md`, `TESTS.md`, tests frontend hôte, éventuellement ce document si une mise à jour est demandée.

Actions : préciser le service statique hôte, la responsabilité du callback, la portée du test réel et les limites toujours ouvertes. Garder les archives explicitement historiques. Ne pas publier les fichiers ignorés ou credentials de l'app.

Validation : relire chaque affirmation contre les fichiers, exécuter la suite et `git diff --check`, examiner le diff sans ajouter de changements non demandés.

### Hors priorité actuelle

Extraction hors de `internal`, nouveau chemin de module, publication, API de configuration générique, global logout, stockage de tokens Shopify, multi-boutiques, popup et déploiement commercial. À traiter seulement sur demande distincte de Contributor.

## 8. Environnement et exécution

### 8.1 Prérequis

- Go compatible avec la directive `go 1.27.2`.
- Node/npm pour les deux tests SDK. Les tests Go ordinaires peuvent fonctionner sans Node si ces intégrations sont ignorées.
- Accès réseau aux modules Go et au registre npm pour une installation neuve.
- Pour le vrai login : dev store et app client configurées, réseau vers Shopify, URL HTTPS de callback et navigateur client.
- Shopify CLI pour valider/publier une modification de l'app, pas pour chaque appel API du studio.

Installation des dépendances du projet :

```powershell
Set-Location D:/Workspaces/shopify-pocketbase-sso
go mod download
npm ci --prefix integration --ignore-scripts --no-audit --no-fund
```

Le projet Shopify imbriqué a son propre `package.json` et sa propre installation CLI. Il n'est pas nécessaire de lancer son template Preact/Vite pour faire fonctionner le backend d'auth Go.

### 8.2 Variables d'environnement

Les noms seulement sont consignés ici ; les valeurs de configuration locale ne sont pas reproduites.

| Nom | Rôle |
| --- | --- |
| `SHOPIFY_SHOP_URL` | Origine de la boutique de confiance |
| `SHOPIFY_REDIRECT_URL` | URL complète de callback choisie et servie par le frontend |
| `SHOPIFY_CUSTOMER_CLIENT_ID` | Identifiant du client Customer Account API |
| `SSO_ALLOW_LOOPBACK_HTTP` | Exception explicite pour fixtures locales loopback, jamais solution de production |
| `RUN_JS_SDK_TEST` | Active les deux intégrations Node/SDK dans la suite Go |

`config.local.ps1` est présent et ignoré. Il n'est pas automatiquement lu comme un `.env` par le programme. Il faut le dot-sourcer dans le terminal PowerShell qui lance le backend. Ne pas le recopier dans le document ou le chat.

Noms historiques à ne pas réintroduire : `STUDIO_URL`, `STUDIO_AUTH_COLLECTION`, `STUDIO_PUBLIC_COLLECTIONS`. Le premier et le troisième appartenaient à la démo/gateway ; le deuxième subsiste dans une migration historique seulement, pas comme option du fournisseur actuel.

### 8.3 Lancement du backend actuel

À faire seulement au moment de la reprise effective des tests, pas comme preuve qu'un serveur tourne déjà :

```powershell
Set-Location D:/Workspaces/shopify-pocketbase-sso
. ./config.local.ps1
go run . serve --http=127.0.0.1:8090 --dir=D:/Workspaces/shopify-pocketbase-sso/pb_data
```

Le `--dir` explicite évite les confusions entre emplacement du binaire, répertoire courant et base utilisée. Le programme fait la découverte Shopify et configure le fournisseur au démarrage. Il ne sert pas actuellement de frontend.

Pour un binaire de test reconstruit depuis les sources retenues :

```powershell
go build -o ./bin/pocketbase-handoff-test.exe .
. ./config.local.ps1
./bin/pocketbase-handoff-test.exe serve --http=127.0.0.1:8090 --dir=D:/Workspaces/shopify-pocketbase-sso/pb_data
```

Ces commandes sont des instructions futures. Ce binaire de nom nouveau n'a pas été créé par la passation. Ne pas lancer simultanément deux serveurs sur la même base pour contourner un conflit de port.

### 8.4 Tests, build et contrôles

```powershell
Set-Location D:/Workspaces/shopify-pocketbase-sso
npm ci --prefix integration --ignore-scripts --no-audit --no-fund
$env:RUN_JS_SDK_TEST = '1'
go test ./... -count=1 -cover
go vet ./...
go build ./...
node --check integration/oauth.mjs
node --check integration/sdk.mjs
git diff --check
```

`RUN_JS_SDK_TEST` est ici un flag de test non secret. Les valeurs des credentials et URL locales ne doivent pas être affichées. Le projet n'a pas de script lint/build frontend à la racine, ni de commande `npm test` définie dans `integration/package.json`.

Tests ciblés utiles :

```powershell
go test ./internal/studio -run 'TestNativeOAuth|TestNativeAuth|TestShopifyAndBuiltIn|TestPrivate|TestSystem' -count=1
go test ./internal/studio -run 'TestOfficial' -count=1
go test ./migrations -count=1
```

Le deuxième appel exige le flag SDK et les dépendances Node déjà préparés. Les fixtures créent leurs propres serveurs HTTP et bases temporaires ; elles n'ont pas besoin que le backend local ou Shopify CLI soient en cours d'exécution.

### 8.5 Configuration Shopify à connaître

Fichier : `D:/Workspaces/shopify-pocketbase-sso/shopify-app/studio-sso/shopify.app.toml`.

État lu, sans reproduire les identifiants ou origines configurés :

- App nommée `Studio SSO`, non embedded.
- Mise à jour automatique des URL en développement désactivée.
- `extension_directories` pointe sur `sso-extensions/`, qui ne contient qu'un `.gitkeep`.
- `web_directories` vide.
- Version webhooks `2026-10`.
- Scope `customer_read_customers`, installation moderne.
- `[auth].redirect_urls` vide, distinct du callback client.
- `[customer_authentication].redirect_uris` contient encore l'ancien chemin `/studio/callback`.
- `application_url` pointe encore sur l'ancien chemin `/studio`.

Le scaffold contient physiquement des extensions Preact/Vite et des exemples, mais le TOML actif dirige la publication vers le dossier vide `sso-extensions`. Il n'y a pas de code métier d'auth Shopify à maintenir dans ces exemples. La logique custom de ce projet est en Go.

Avant tout travail dans ce répertoire, lire `shopify-app/studio-sso/AGENTS.md`. Il impose l'usage du Shopify AI Toolkit pour le travail plateforme. Ce fichier n'impose pas de délégation à des sous-agents.

Commande de validation, à lancer depuis le dossier app :

```powershell
Set-Location D:/Workspaces/shopify-pocketbase-sso/shopify-app/studio-sso
shopify app config validate --json
```

Elle a été rapportée valide dans l'historique. Elle n'a pas été relancée pour la passation. Un `shopify app deploy` modifie l'état externe ; il ne doit pas être confondu avec une compilation locale ou lancé pendant une simple inspection.

### 8.6 Particularités Windows et PowerShell

- La configuration doit être chargée dans le même processus PowerShell que le lancement. Un nouveau terminal n'hérite pas des affectations faites dans un autre terminal.
- Utiliser des chemins explicites et `-LiteralPath` pour les opérations sensibles. Ne pas supprimer récursivement une racine de workspace ou un chemin calculé non vérifié.
- Pour chercher les fichiers, utiliser `rg --files` et `rg -g '*.go'` sur un dossier ; ne pas compter sur l'expansion Unix des glob patterns dans les arguments Windows.
- Pour un processus de fond, vérifier l'exécutable, le port et le PID courant avant de l'arrêter. Un PID historique peut avoir été réutilisé.
- Si un processus est lancé avec `Start-Process`, utiliser une fenêtre masquée et conserver le PID de la nouvelle instance.
- Ne pas mettre de token, mot de passe, code e-mail ou verifier dans la ligne de commande ou l'historique shell.
- Ne pas activer SQL debug par commodité sur les données réelles. `DefaultDev: false` a été choisi pour éviter les logs de profils.
- Aucun outillage navigateur pilotable n'est garanti dans T3 Code. En cas d'absence, préparer l'environnement et demander à Contributor de réaliser la saisie du code e-mail dans son navigateur, puis recueillir seulement un résultat non sensible.

## 9. Informations critiques à conserver

### 9.1 Ne pas modifier implicitement

- Le choix non-Plus et les nouveaux comptes e-mail.
- La collection d'auth `users`, son schéma, ses règles et ses autres fournisseurs.
- Le nom `_shopifyIdentities`, son statut système, ses règles verrouillées et ses index.
- Les ID `users`, les relations métier et les liaisons `_externalAuths` existantes.
- Le refus de fusion par e-mail ou par session `Authorization` non liée.
- L'absence de tokens Shopify persistés/exposés et l'absence de champs de noms synchronisés.
- Le contrôle du nonce, l'adaptation numérique après signature et les diagnostics sans valeurs sensibles.
- La capture de réponse jusqu'à la fin des hooks.
- L'absence d'API de session parallèle, de cookie studio ou de stockage serveur des tentatives.
- Le report explicite du packaging.

Préserver aussi `pb_data`, `pb_backups`, la configuration locale et le dépôt Shopify imbriqué. Ils ne sont pas tous récupérables depuis le dépôt principal.

### 9.2 Erreurs de reprise à éviter

1. Lire les archives puis réimplémenter `/studio/session` ou les anciens callbacks serveur.
2. Prendre les helpers `fixture.callback` pour une API à exposer.
3. Annoncer le frontend présent parce qu'un binaire porte son nom.
4. Confondre le test réel de la version intermédiaire avec celui du dernier contrat sans cookies.
5. Définir seulement `/auth/callback` dans le JS sans aligner l'URL exacte backend et Shopify.
6. Croire qu'un simple `_` ou `hidden` remplace les règles privées de la collection.
7. Marquer `verified=true` sur la seule base d'une adresse de profil non corroborée par le claim.
8. Corriger un conflit `409` en autorisant une fusion implicite.
9. Transformer un sujet numérique en nombre JS/Go flottant ou authentifier un payload non signé.
10. Déduire que `authRefresh` prouve une connexion Shopify encore active.
11. Croire que le passage à une auth native rend le package `internal/studio` importable depuis n'importe quel module.
12. Exposer toute l'API d'administration sur un tunnel de test parce que l'ancienne gateway n'existe plus.
13. Relancer les migrations de nettoyage du prototype sur un projet hôte.
14. Ajouter des champs nom/prénom à `users` alors que leur retrait a été demandé.

### 9.3 Questions encore ouvertes

- Contributor préfère-t-il finalement les quelques lignes de service statique dans `main.go` ou un helper hôte séparé ? Le fichier séparé n'a jamais été une obligation.
- Faut-il recréer le frontend absent exactement comme la tentative rapportée ou le réduire encore ? Le besoin validé est une page minimale dans `pb_public`, pas la conservation d'une structure perdue.
- Quelle URL HTTPS de test est disponible maintenant ? L'ancienne URL de Quick Tunnel est historique et aucun tunnel ne tourne.
- Quand coordonner la modification du callback Shopify vers un chemin neutre ? Aucun changement externe n'a été fait pendant la passation.
- Quelles permissions de données protégées seront accordées sur la future boutique commerciale ? Le test dev store ne les démontre pas.
- Une interface MFA complète, un maximum de durée depuis la dernière connexion Shopify, une vraie déconnexion globale ou une relecture de profil hors login seront-ils demandés ? Pas dans le périmètre immédiat.
- Quel chemin de module et quelle API d'installation pour la future librairie ? Décision volontairement différée, même si un remote Git existe déjà.

### 9.4 Sécurité du document et de l'assistance

Ce document ne contient ni credential local, ni token, ni mot de passe, ni adresse de client, ni valeur de code ou de verifier. Les noms d'environnement, versions, chemins, noms de collections et décisions sont les informations utiles à transférer.

Ne pas copier intégralement `config.local.ps1`, les fichiers `.shopify`, les sessions de la CLI, les lignes de base ou les sorties d'erreur SDK brutes dans une prochaine réponse. Si une vérification de données est nécessaire, préférer compteurs, égalités et indicateurs de présence sans imprimer les valeurs.

Pour cette passation, KISS a guidé la séparation entre le cœur d'auth, l'hôte de test et le packaging différé. Les références Shopify ont servi à vérifier le contrat d'authentification, pas à publier une nouvelle app ni à changer l'état de la boutique.

## INSTRUCTIONS POUR REPRENDRE LE TRAVAIL

1. **Lire dans cet ordre** : les avertissements initiaux et la section 6 de ce document ; `README.md` ; `TESTS.md` ; `main.go` ; `internal/studio/studio.go` ; `provider.go` ; `store.go` ; `accounts.go` ; `auth_response.go` ; `shopify.go` ; puis les tests concernés. Lire `shopify-app/studio-sso/AGENTS.md` avant d'agir dans le projet Shopify. Utiliser les deux documents `docs/prototype-*` uniquement pour l'historique.

2. **Objectif actuel** : achever un frontend hôte minimal dans `pb_public` pour tester réellement le dernier parcours natif Shopify/PocketBase, sans remettre de logique de démo dans `internal/studio`. La librairie publiable reste une étape future.

3. **Point d'arrêt** : le cœur est à HEAD `75d85a7` et les tests actuels passent. Les fichiers frontend de la tentative rapportée sont absents, aucun handler statique n'est enregistré et aucun serveur/tunnel du projet ne tourne à l'inspection. Le callback configuré reste sur l'ancien chemin, alors que le README illustre `/auth/callback`.

4. **Tâche immédiate** : refaire un état Git/fichiers/processus non mutatif, puis répondre à la dernière question de Contributor. Expliquer que l'exécutable officiel sert `pb_public`, mais que son hôte Go personnalisé doit enregistrer `apis.Static`. Le fichier `frontend.go` est facultatif. Ensuite reprendre la création du frontend minimal déjà demandé, une fois le contexte de reprise confirmé, sans supposer que les fichiers manquants sont récupérables depuis HEAD.

5. **Décisions à respecter** : non-Plus, nouveaux comptes e-mail, `users` fixe, `_shopifyIdentities` privée système, pas de noms Shopify, pas de fusion automatique, pas de credentials externes dans le navigateur, pas de session bridge/cookies maison/endpoints de préparation, redirection via `listAuthMethods` et `authWithOAuth2Code`, callback choisi par l'hôte, politiques PocketBase natives conservées, packaging différé. Ne pas modifier les données ou les fichiers non suivis pour faire du ménage sans demande.

6. **Validation attendue** : exécuter la suite Go avec les deux tests SDK activés, `go vet`, build et contrôles JS ; ajouter des tests pour le frontend et les routes statiques ; aligner les trois callbacks ; puis refaire les scénarios connecté/anonyme sur la vraie boutique avec Contributor. Confirmer l'accès API natif et l'auth store, pas seulement le rendu d'un profil. Conserver les comptes et liaisons, ne rien afficher de sensible et noter explicitement ce qui est simulé, réel ou encore non testé.
