# Shopify app setup

Follow this guide when [integrating SSO into your existing PocketBase application](../README.md#integration). Have the frontend's HTTPS URL and its callback URL ready, using a deployment or a public HTTPS tunnel.

1. In the store's Shopify admin, open **Settings > Customer accounts** and make sure the store uses customer accounts with email-code sign-in. Note the store origin, such as `https://my-store.myshopify.com`, for `ShopURL`. [Customer accounts](https://help.shopify.com/en/manual/customers/customer-accounts/manage).
2. In the [Dev Dashboard](https://dev.shopify.com/dashboard), select **Apps > Create app > Start from Dev Dashboard** and fill in the creation form:

   - **App name**: choose a name, such as `PocketBase SSO`.
   - **Version name**: leave empty or use a label such as `v1`.
   - **App URL**: enter your frontend's HTTPS URL, such as `https://my-frontend.example`. If you do not have one yet, use `https://example.com` temporarily and replace it before testing sign-in. A public HTTPS tunnel to your local frontend works for development.
   - **Embed app in Shopify admin**: uncheck this and keep it unchecked after configuring the real URLs. The frontend runs as a standalone site; this SSO flow does not need to render inside Shopify admin.
   - **Allowed redirection URL(s)**: leave empty for this integration. This field configures app installation OAuth redirects; the customer sign-in callback is configured separately in step 4.
   - **Use legacy install flow**: leave unchecked.
   - **Scopes**: enter `customer_read_customers` to read the signed-in customer's identity and email address.
   - **Webhooks API version**: keep the default. This package does not subscribe to Shopify webhooks.

   Click **Create app**, then open **App settings** and copy the **Client ID** from the credentials card into `ClientID`. [Create an app](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard).
3. Before installing the app on the target store, choose how it will be distributed. This setting controls which stores can install the app. For an app dedicated to one merchant's store, use **Custom distribution**:

   - In the Dev Dashboard, open the app you just created and go to its **Home** page.
   - Find the **Distribution** card and click **Select distribution method**.
   - Select **Custom distribution**, then confirm. This lets you generate an installation link for the target store without publishing the app on the Shopify App Store.

   Shopify does not allow you to change this choice later. If you select **Public distribution**, Shopify approval is required; moving to custom distribution later means creating a new app and replacing `ClientID` in your application.

   The **Distribution** card only appears for apps owned by a Shopify Partner organization, such as a developer or agency organization. If you created the app in the store's own organization, there is no distribution choice to make: use the **Installs** card at step 6 instead. See [Select a distribution method](https://shopify.dev/docs/apps/launch/distribution/select-distribution-method).

4. With [Shopify CLI](https://shopify.dev/docs/apps/build/cli-for-apps/migrate-from-dashboard), run `shopify app config link` in a folder dedicated to the Shopify configuration and select this app. In the generated `shopify.app.toml`, set `application_url` to your frontend's HTTPS URL, set `embedded = false` and update these sections:

   ```toml
   [access_scopes]
   scopes = "customer_read_customers"
   
   [customer_authentication]
   redirect_uris = ["https://my-frontend.example/redirect.html"]
   ```

   Replace the example URLs with the frontend and callback URLs you prepared earlier. The callback must match `RedirectURL` exactly, including the value you will supply through `SHOPIFY_CUSTOMER_CALLBACK_URL` in the [PocketBase environment configuration](../README.md#4-configure-the-environment). Adding it to the dashboard's **Allowed redirection URL(s)** field does not register it for customer sign-in; it must be listed under `customer_authentication.redirect_uris`. Keep `embedded = false` when updating the URLs. [Shopify configuration](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).
5. Ensure the app can access [protected customer data](https://shopify.dev/docs/apps/launch/protected-customer-data), including **Email**. For public apps, open your app in the Partner Dashboard under **Apps > your app > API access requests**. Under **Protected customer data access**, select **Request access** and declare the fields you need.
6. Publish the configuration with `shopify app deploy`, then install the app:

   - For **Custom distribution**, open the app's **Home** page in the Dev Dashboard, then select **Manage custom install link** on the **Distribution** card. Enter the target store's domain, generate the link and have the store owner open it to approve installation.
   - For an app created in the store's own organization, select **Install app** on the **Installs** card, choose the target store and select **Install** to approve its permissions.

This flow uses the app's Client ID with PKCE. No client secret or Admin API token is needed. [Customer Account API authentication](https://shopify.dev/docs/api/customer/2026-10#authentication).

Return to the [README environment configuration](../README.md#4-configure-the-environment) to set the store origin, callback URL and Client ID, then register the SSO service and test sign-in.
