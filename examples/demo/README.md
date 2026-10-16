# Démonstration PocketBase

Cet exécutable illustre la bibliothèque Go à la racine du dépôt. Il possède les pages HTML, leur CSP et la lecture des variables d’environnement. Les commandes ci-dessous se lancent depuis la racine du dépôt.

## Développement avec mise

Le projet utilise [mise-en-place](https://mise.jdx.dev/) pour Go `1.27.2`, Node `24.14.1`, cloudflared `2026.10.1`, Shopify CLI `4.9.4`, les variables locales et les tâches. npm est fourni avec Node. Les versions sont fixées dans `mise.toml`. Les outils sont installés dans le stockage de mise, sans copie dans le dépôt. `bin/` est réservé aux résultats de compilation régénérables de l'application ; les anciens exécutables du prototype ont été supprimés.

Après installation de mise, depuis la racine du dépôt :

```powershell
mise trust
mise install
mise run setup
```

Les variables sont directement dans `[env]` de `mise.toml` : URL de boutique, URL de callback, Client ID public et exception HTTP loopback. L'intégration utilise exclusivement un client public PKCE, sans secret client à demander ou configurer. Aucun fichier local ou fichier d'exemple n'est nécessaire pour ce parcours.

mise charge automatiquement ces fichiers avec `mise run` et `mise exec`, sans script PowerShell à sourcer. L'ancien `config.local.ps1` a été retiré après migration.

| Commande | Effet |
| --- | --- |
| `mise run setup` | Télécharge les modules Go et installe le SDK fixé pour les tests |
| `mise run dev` | Lance PocketBase et le frontend sur `127.0.0.1:8090` |
| `mise run tunnel` | Lance le tunnel nommé sur `sso.example.com` |
| `mise run tunnel:login` | Autorise ce poste auprès du compte Cloudflare |
| `mise run dev:public` | Lance le serveur et le tunnel en parallèle dans le même terminal |
| `mise run test` | Prépare les dépendances et lance toute la suite, avec les trois intégrations SDK |
| `mise run build` | Compile le binaire dans `bin/` |
| `mise run check` | Lance tests, analyse Go, build, contrôles JavaScript et diff Git |

`mise tasks` liste aussi les tâches individuelles `vet`, `check:js` et `check:diff`. Les tâches partent de la racine du projet et ne nécessitent pas d'activer mise dans le profil du shell. Les tests utilisent leurs propres serveurs et bases temporaires ; ils n'exigent pas de configuration Shopify locale. `mise.local.toml`, ignoré par Git, reste utilisable pour d'éventuelles surcharges personnelles.

## Configuration et hôte Go

`examples/demo/main.go` reste un hôte PocketBase minimal. Il initialise `shopifysso.New` dans `OnServe`, puis appelle `Register`. Ce dernier ne déclare aucune route : il adapte uniquement les requêtes OAuth natives.

L'hôte enregistre une route générale `GET /{path...}` avec `apis.Static` pour servir directement les fichiers de `examples/demo/pb_public`, comme le binaire officiel. `/` sert `index.html` et `/redirect.html` sert `redirect.html`, sans route spéciale de callback ni filtre de préfixes personnalisé. Les routes API/admin natives gardent leur priorité. Le fallback vers index pour un fichier absent reste désactivé, car ces pages ne sont pas une SPA. Les en-têtes de protection et l’exclusion des requêtes statiques réussies des logs d’activité sont conservés.

## Page de test locale

`examples/demo/pb_public/index.html` et `examples/demo/pb_public/redirect.html` contiennent chacun leur CSS et leur JavaScript inline. Le SDK officiel est importé depuis jsDelivr, fixé à `0.28.1`. Aucune copie du SDK n'est conservée dans `examples/demo/pb_public`. La CSP autorise ce fichier CDN précis et les empreintes SHA-256 des blocs inline. Si ces blocs changent, mettre à jour leurs empreintes dans `examples/demo/main.go` ; les tests signalent un décalage. Le navigateur doit pouvoir joindre ce CDN ; les tests Node utilisent la même version installée dans `tests/sdk`.

Les deux pages reprennent le flux [Manual code exchange de PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), avec les [deux fonctions testées dans le README de la bibliothèque](../../README.md#exemple-frontend-minimal). Une tentative silencieuse est faite à l'arrivée sans session locale. Le callback ne la relance jamais automatiquement. Les boutons permettent une connexion interactive, un test silencieux explicite, la vérification de l'API et la déconnexion locale. Après connexion, le callback revient à `/`, où une nouvelle instance du SDK restaure son auth store puis appelle `authRefresh()` et `getOne()` sur son propre compte.

Les erreurs attendues de `prompt=none` proposent le bouton de connexion. Une MFA est signalée, sans interface de second facteur. Les tokens, codes OAuth, verifiers et identifiants Shopify bruts ne sont pas affichés. Le callback est servi en `no-store`, avec `Referrer-Policy: no-referrer` et une CSP adaptée au CDN.

Lancement depuis la racine du dépôt, lorsque les URL de test sont alignées :

```powershell
mise run dev
```

`--publicDir` vaut `./examples/demo/pb_public` par défaut, relatif au répertoire de travail, y compris avec un binaire placé dans `bin`. Utiliser un chemin absolu évite toute ambiguïté.

La tâche `dev` fournit les chemins absolus de `pb_data` et `examples/demo/pb_public` à partir de la racine du projet.

L'adresse de test fixe est `https://sso.example.com`. Le tunnel Cloudflare nommé `studio-sso-dev` et son entrée DNS ont été créés. `examples/demo/cloudflared.yml` contient son identifiant et les règles d'exposition. Seuls `/`, `/redirect.html`, `/api/health` et les endpoints natifs `users` nécessaires à cette page sont transmis au serveur. Les autres chemins, dont l'admin et les API superuser, renvoient 404 au niveau du tunnel. Ces restrictions concernent l'accès public ; PocketBase reste inchangé en local.

Pour lancer le serveur et le tunnel dans un seul terminal :

```powershell
mise run dev:public
```

Cette tâche regroupe `dev` et `tunnel` via les dépendances natives de mise, exécutées en parallèle. Garder au moins deux jobs disponibles, valeur par défaut sur ce poste ; si nécessaire, lancer `mise run --jobs 2 dev:public`. Les tâches `dev` et `tunnel` restent utilisables séparément. L'adresse du tunnel nommé reste identique après un arrêt et un redémarrage.

Le poste actuel a été autorisé avec `mise run tunnel:login`. Le certificat du compte et le fichier JSON privé du tunnel sont conservés dans `~/.cloudflared`, hors du dépôt. Un autre poste doit disposer des credentials du tunnel dans ce répertoire ; le certificat de connexion seul ne remplace pas ce fichier. Ne pas copier leur contenu dans Git ou dans les logs.

Le frontend, `SHOPIFY_REDIRECT_URL` et le callback Shopify publié utilisent `https://sso.example.com/redirect.html`. La version `studio-sso-5` a été publiée le 16 octobre 2026 ; une nouvelle récupération depuis Shopify confirme cette URL et l’absence de l’ancien callback. Redémarrer le serveur et le tunnel avec `mise run dev:public` pour charger les changements locaux, puis terminer la connexion réelle afin de vérifier l’accès API.

La configuration de l'app est conservée dans `examples/demo/shopify-app/shopify.app.toml`, récupérée depuis la version publiée avant modification du seul callback client. Son état CLI local `.shopify/` est ignoré par Git. Pour la valider :

```powershell
mise exec -- shopify app config validate --path examples/demo/shopify-app --json
```

Si Shopify affiche `redirect_uri mismatch` malgré un callback publié exact, vérifier qu'aucun ancien aperçu `shopify app dev` ne remplace encore la version active sur la dev store. Cet aperçu persiste après l'arrêt de la CLI. Pour restaurer la version publiée sur notre boutique de test :

```powershell
mise exec -- shopify app dev clean --path examples/demo/shopify-app --store example-store.myshopify.com
```

Cette correction a été appliquée le 16 octobre 2026. Le parcours silencieux anonyme revient ensuite au studio et le bouton interactif ouvre le formulaire Shopify réel. Le login complet avec code e-mail reste à confirmer par l'utilisateur. Lancer `mise run dev:public` ne crée pas de preview Shopify.


## Migrations historiques

`examples/demo/migrations` conserve les migrations du prototype et leurs tests. Elles ne sont importées ni par la bibliothèque ni par cet exécutable. Un projet neuf n’en a pas besoin. La bibliothèque crée et verrouille seulement sa collection privée `_shopifyIdentities` lors de `New`. Les données locales existantes restent dans `pb_data` à la racine du dépôt.
