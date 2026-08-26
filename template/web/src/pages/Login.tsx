// Login placeholder: the template ships deny-all auth, so there is
// nothing to log in to yet. Wire your host auth (authstub/middleware.go)
// and turn this into your real login flow.
export default function Login() {
  return (
    <section className="card">
      <h1>Login</h1>
      <p>
        This starter denies every API request by default. Wire your host
        authentication in <code>authstub/middleware.go</code> (the single
        Gate slot), then build the real login flow here.
      </p>
      <button disabled>Sign in (not wired)</button>
    </section>
  )
}
