# Authentification Shopify dans PocketBase

L'intégration ajoute un fournisseur `shopify` au parcours OAuth2 natif de PocketBase. Le frontend utilise `listAuthMethods()`, redirige vers Shopify, vérifie le retour puis appelle `authWithOAuth2Code()`. Le SDK reçoit le token PocketBase et met son auth store à jour.

Le module Go `studio-sso` et le package `internal/studio` restent en place. Aucun packaging ni publication n'est préparé.

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

## Responsabilités

- PocketBase génère le state, le verifier et le challenge PKCE via son endpoint natif `users/auth-methods`.
- Le frontend conserve ces paramètres dans le `sessionStorage` de l'onglet, vérifie le state et son expiration au retour, puis les supprime avant tout échange.
- Le fournisseur Go échange le vrai code Shopify, valide l'ID token et récupère l'identifiant client et l'e-mail.
- PocketBase crée ou retrouve le compte `users`, conserve la liaison dans `_externalAuths` et applique ses règles, hooks, MFA et enrichissements natifs.

Il n'existe plus de `/api/shopify/auth/start`, de `/api/shopify/auth/complete`, de cookie OAuth, ni de transactions de connexion stockées en mémoire côté serveur. Le package `internal/studio` ne fournit aucune page HTML ni API de session parallèle. L'hôte sert deux pages de test minimales dans `pb_public`.

Le state est désormais vérifié par le frontend, pas par le backend. La possession du verifier PKCE protège l'échange du code. Les codes à usage unique et leur expiration sont contrôlés par Shopify. Le délai de cinq minutes dans l'exemple est un contrôle local supplémentaire, pas une expiration imposée par notre serveur.

## Configuration et hôte Go

`main.go` reste un hôte PocketBase minimal. Il initialise `studio.New` dans `OnServe`, puis appelle `Register`. Ce dernier ne déclare aucune route : il adapte uniquement les requêtes OAuth natives.

L'hôte enregistre une route générale `GET /{path...}` avec `apis.Static` pour servir directement les fichiers de `pb_public`, comme le binaire officiel. `/` sert `index.html` et `/redirect.html` sert `redirect.html`, sans route spéciale de callback ni filtre de préfixes personnalisé. Les routes API/admin natives gardent leur priorité. Le fallback vers index pour un fichier absent reste désactivé, car ces pages ne sont pas une SPA. Les en-têtes de protection et l’exclusion des requêtes statiques réussies des logs d’activité sont conservés.

## Page de test locale

`pb_public/index.html` et `pb_public/redirect.html` contiennent chacun leur CSS et leur JavaScript inline. Le SDK officiel est importé depuis jsDelivr, fixé à `0.28.1`. Aucune copie du SDK n'est conservée dans `pb_public`. La CSP autorise ce fichier CDN précis et les empreintes SHA-256 des blocs inline. Si ces blocs changent, mettre à jour leurs empreintes dans `main.go` ; les tests signalent un décalage. Le navigateur doit pouvoir joindre ce CDN ; les tests Node utilisent la même version installée dans `integration`.

Les deux pages reprennent le flux [Manual code exchange de PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), avec les deux fonctions testées ci-dessous. Une tentative silencieuse est faite à l'arrivée sans session locale. Le callback ne la relance jamais automatiquement. Les boutons permettent une connexion interactive, un test silencieux explicite, la vérification de l'API et la déconnexion locale. Après connexion, le callback revient à `/`, où une nouvelle instance du SDK restaure son auth store puis appelle `authRefresh()` et `getOne()` sur son propre compte.

Les erreurs attendues de `prompt=none` proposent le bouton de connexion. Une MFA est signalée, sans interface de second facteur. Les tokens, codes OAuth, verifiers et identifiants Shopify bruts ne sont pas affichés. Le callback est servi en `no-store`, avec `Referrer-Policy: no-referrer` et une CSP adaptée au CDN.

Lancement depuis la racine du dépôt, lorsque les URL de test sont alignées :

```powershell
mise run dev
```

`--publicDir` vaut `./pb_public` par défaut, relatif au répertoire de travail, y compris avec un binaire placé dans `bin`. Utiliser un chemin absolu évite toute ambiguïté.

La tâche `dev` fournit les chemins absolus de `pb_data` et `pb_public` à partir de la racine du projet.

L'adresse de test fixe est `https://sso.example.com`. Le tunnel Cloudflare nommé `studio-sso-dev` et son entrée DNS ont été créés. `cloudflared.yml` contient son identifiant et les règles d'exposition. Seuls `/`, `/redirect.html`, `/api/health` et les endpoints natifs `users` nécessaires à cette page sont transmis au serveur. Les autres chemins, dont l'admin et les API superuser, renvoient 404 au niveau du tunnel. Ces restrictions concernent l'accès public ; PocketBase reste inchangé en local.

Pour lancer le serveur et le tunnel dans un seul terminal :

```powershell
mise run dev:public
```

Cette tâche regroupe `dev` et `tunnel` via les dépendances natives de mise, exécutées en parallèle. Garder au moins deux jobs disponibles, valeur par défaut sur ce poste ; si nécessaire, lancer `mise run --jobs 2 dev:public`. Les tâches `dev` et `tunnel` restent utilisables séparément. L'adresse du tunnel nommé reste identique après un arrêt et un redémarrage.

Le poste actuel a été autorisé avec `mise run tunnel:login`. Le certificat du compte et le fichier JSON privé du tunnel sont conservés dans `~/.cloudflared`, hors du dépôt. Un autre poste doit disposer des credentials du tunnel dans ce répertoire ; le certificat de connexion seul ne remplace pas ce fichier. Ne pas copier leur contenu dans Git ou dans les logs.

Le frontend et `SHOPIFY_REDIRECT_URL` utilisent désormais `https://sso.example.com/redirect.html`. **Publication Shopify à faire** : la version active `studio-sso-4` et `shopify-app/shopify.app.toml` conservent encore l’ancien `/auth/callback`. La prochaine étape sera de remplacer `customer_authentication.redirect_uris` par la nouvelle URL et de publier une version. Le login réel ne pourra être retesté qu’après cet alignement et le redémarrage du serveur et du tunnel. Aucune publication n’a été effectuée pendant le passage au routage statique général.

La configuration de l'app est conservée dans `shopify-app/shopify.app.toml`, récupérée depuis la version publiée avant modification du seul callback client. Son état CLI local `.shopify/` est ignoré par Git. Pour la valider :

```powershell
mise exec -- shopify app config validate --path shopify-app --json
```

Si Shopify affiche `redirect_uri mismatch` malgré un callback publié exact, vérifier qu'aucun ancien aperçu `shopify app dev` ne remplace encore la version active sur la dev store. Cet aperçu persiste après l'arrêt de la CLI. Pour restaurer la version publiée sur notre boutique de test :

```powershell
mise exec -- shopify app dev clean --path shopify-app --store example-store.myshopify.com
```

Cette correction a été appliquée le 16 octobre 2026. Le parcours silencieux anonyme revient ensuite au studio et le bouton interactif ouvre le formulaire Shopify réel. Le login complet avec code e-mail reste à confirmer par l'utilisateur. Lancer `mise run dev:public` ne crée pas de preview Shopify.

## Configuration du fournisseur

| Paramètre Go | Variable d'environnement | Rôle |
| --- | --- | --- |
| `ShopURL` | `SHOPIFY_SHOP_URL` | Origine HTTPS de la boutique de confiance |
| `RedirectURL` | `SHOPIFY_REDIRECT_URL` | Callback du frontend, enregistré à l'identique dans Shopify |
| `ClientID` | `SHOPIFY_CUSTOMER_CLIENT_ID` | Client Customer Account API |
| `AllowLoopbackHTTP` | `SSO_ALLOW_LOOPBACK_HTTP=1` | Exception de développement limitée au loopback |

Le POST OAuth exige l'en-tête `Origin` correspondant à l'origine de `RedirectURL`, que le navigateur envoie automatiquement. Cette origine est celle du frontend, pas nécessairement celle de l'API. Si le frontend et l'API ont des origines différentes, leur configuration CORS doit autoriser les requêtes nécessaires. Aucun cookie ni en-tête manuel n'est requis côté navigateur. Un proxy doit transmettre l'en-tête `Origin` sans le réécrire.

L'application hôte choisit et sert sa page de callback. Aucun chemin n'est imposé ou enregistré par le module Go. `/redirect.html` dans l'exemple ci-dessous est une convention d'illustration, pas une route fournie par la bibliothèque. L'URL complète choisie doit être identique dans le frontend, dans `RedirectURL` et dans la configuration Shopify.

La collection d'auth est fixée à `users` et doit exister. L'intégration n'y ajoute aucun champ et ne choisit aucune règle d'accès. Elle active OAuth2 et configure uniquement `shopify`, sans modifier les autres fournisseurs, le mot de passe, l'OTP, la MFA ou la durée des tokens.

`_shopifyIdentities` est une collection système avec les champs `user`, `issuer`, `subject` et `customerId`. Ses cinq règles API sont verrouillées. Les identifiants bruts sont réservés au serveur et aux superusers. Les prénoms et noms ne sont ni demandés ni synchronisés.

## Exemple frontend minimal

Le frontend suit la séparation de l’exemple PocketBase « Manual code exchange » : `index.html` lance la connexion et vérifie l’accès API ; `redirect.html` vérifie le retour OAuth et échange le code. Les deux fichiers conservent leurs scripts et styles inline, avec le SDK officiel via CDN.

Le callback est directement le fichier statique `/redirect.html`. Après succès, cette page revient à `/`, où le SDK restaure son auth store et vérifie l’accès API. En cas d’erreur ou d’absence de session Shopify, elle affiche un message et un lien `/?login` vers les boutons de connexion, sans nouvelle tentative silencieuse automatique.

Dans un frontend avec bundler et le SDK officiel installé :

```javascript
import PocketBase from 'pocketbase';

const pb = new PocketBase(window.location.origin);
// Chemin choisi et servi par l'application hôte, pas par le module Go.
// L'URL complète doit correspondre à SHOPIFY_REDIRECT_URL et au callback Shopify.
const redirectURL = new URL('/redirect.html', window.location.origin).href;
```

Les deux fonctions suivantes suffisent pour le passage OAuth. Elles appartiennent au frontend hôte ; aucun helper JavaScript n'est ajouté au backend Go.

<!-- native-redirect-example:start -->
```javascript
async function loginWithShopify(silent = false) {
  const methods = await pb.collection('users').listAuthMethods();
  const provider = methods.oauth2.providers.find(p => p.name === 'shopify');
  if (!provider) throw new Error('Shopify non configuré.');

  sessionStorage.setItem('shopify.oauth', JSON.stringify({
    state: provider.state,
    verifier: provider.codeVerifier,
    expires: Date.now() + 5 * 60 * 1000,
  }));

  const url = new URL(provider.authURL + encodeURIComponent(redirectURL));
  if (silent) url.searchParams.set('prompt', 'none');
  location.assign(url.href);
}

async function completeShopifyLogin() {
  const params = new URLSearchParams(location.search);
  history.replaceState(null, '', location.pathname);

  const saved = sessionStorage.getItem('shopify.oauth');
  sessionStorage.removeItem('shopify.oauth');
  const flow = JSON.parse(saved || 'null');

  if (!flow || !Number.isFinite(flow.expires) ||
      flow.expires <= Date.now() || flow.state !== params.get('state')) {
    throw new Error('Retour Shopify invalide ou expiré.');
  }

  if (params.has('error')) return { error: params.get('error') };
  const code = params.get('code');
  if (!code) throw new Error('Code Shopify manquant.');

  // Empêcher la liaison avec un autre compte déjà connecté.
  pb.authStore.clear();
  return pb.collection('users').authWithOAuth2Code(
    'shopify', code, flow.verifier, redirectURL,
  );
}
```
<!-- native-redirect-example:end -->

Utilisation :

- À l'arrivée sans compte PocketBase connecté : appeler `loginWithShopify(true)` une seule fois.
- Au clic sur le bouton de connexion : appeler `loginWithShopify(false)`.
- Sur le callback : appeler `completeShopifyLogin()`. En cas de `login_required`, `interaction_required` ou `consent_required`, proposer un retour aux boutons de connexion. En cas de `access_denied`, afficher l'annulation. Ne jamais relancer automatiquement une tentative silencieuse depuis le callback.
- Gérer les erreurs des promesses dans l'interface. Ne pas afficher directement des paramètres OAuth arbitraires, ni journaliser le code, le verifier ou les tokens.
- Après un succès, le SDK a déjà alimenté `pb.authStore`. Les appels CRUD, le refresh et le realtime utilisent l'authentification native.

Le frontend doit servir le callback avec `Cache-Control: no-store` et `Referrer-Policy: no-referrer`, sans analytics, avec le seul SDK externe fixé depuis le CDN. Il doit effacer le code de l'adresse avant tout traitement. Le `sessionStorage` contient temporairement un secret PKCE : protéger ce frontend contre les injections de script est indispensable.

Le code de cet exemple est exécuté tel quel par le test d'intégration avec le SDK officiel. `loginWithShopify` se trouve dans `pb_public/index.html` et `completeShopifyLogin` dans `pb_public/redirect.html` ; un test vérifie leur parité.

## Sécurité et limites

Le fournisseur ajoute automatiquement un nonce à l'URL native : le nonce est le challenge S256 PKCE, jamais le verifier secret. C'est une valeur imprévisible et différente pour chaque tentative. Au moment de l'échange, le serveur recalcule le challenge depuis le verifier reçu et compare le nonce de l'ID token signé. Aucune valeur attendue n'a besoin d'être conservée côté serveur.

Le serveur conserve les contrôles de format PKCE, callback exact, client, origine, signature JWKS, algorithme, issuer, audience, expiration, nonce et `at_hash` si présent. Les sujets Shopify numériques sont normalisés après vérification de signature, sans perte de précision. Le POST natif et la réponse des méthodes d'auth sont marqués `no-store`.

PocketBase impose un champ `ClientSecret` non vide dans sa configuration de fournisseur. Le marqueur public interne `public-pkce-no-secret` satisfait uniquement cette validation. Il n'est jamais envoyé à Shopify : le client OAuth d'échange ne possède aucun secret, et le simulateur refuse tout paramètre `client_secret` ou en-tête d'authentification du client.

Un e-mail identique ou un compte présent dans `Authorization` ne suffisent pas à fusionner une identité Shopify avec un compte existant. Seule une liaison exacte est acceptée ; un conflit renvoie HTTP 409. Les tokens Shopify ne sont ni exposés dans les métadonnées OAuth ni conservés pour des appels ultérieurs.

La mémoire tampon de réponse OAuth empêche la fuite d'un JWT lorsqu'un hook refuse après `e.Next()`. Elle ne fabrique aucun token et conserve les réponses natives, notamment les challenges MFA. Le frontend doit gérer une MFA si le projet l'active ; l'exemple ci-dessus ne fournit pas cet écran.

Le parcours documenté et testé est la redirection manuelle avec `authWithOAuth2Code`. La popup `authWithOAuth2` n'est pas activée par cette étape, car elle utilise un autre callback. Aucun iframe silencieux, contrôle périodique de session Shopify ni synchronisation globale des déconnexions n'est ajouté.

Les sessions PocketBase et Shopify restent indépendantes. `pb.authStore.clear()` déconnecte localement le studio ; il ne déconnecte pas Shopify et ne révoque pas les copies déjà émises du token. Une nouvelle tentative silencieuse peut reconnecter le client si Shopify est toujours connecté.

L'intégration ne conserve plus de transaction OAuth locale qui impose de rester sur la même instance Go. Cela ne configure pas pour autant une infrastructure PocketBase multi-instance ou un stockage partagé.

[Parcours OAuth PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), [SDK et auth store](https://github.com/pocketbase/js-sdk#auth-store), [authentification Shopify](https://shopify.dev/docs/api/customer/2026-10#authentication).

## Tests et historique

Voir [TESTS.md](TESTS.md). Le fournisseur simulé est réservé aux tests ; aucune fausse boutique ni gateway n'est distribuée.

Les migrations historiques de `migrations` restent testées mais ne sont pas importées par `main.go` ou le cœur. Ne pas réutiliser leurs anciens nettoyages sur un autre projet. Ce changement ne migre ni les comptes ni les liaisons Shopify existantes.

L'enquête et les validations antérieures restent dans [docs/prototype-history.md](docs/prototype-history.md) et [docs/prototype-tests.md](docs/prototype-tests.md). Leurs anciens parcours et URL ne décrivent pas l'interface actuelle.
