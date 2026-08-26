import { Link, Route, Routes } from "react-router-dom"
import Login from "./pages/Login"
import Products from "./pages/Products"

export default function App() {
  return (
    <div className="app">
      <header>
        <span className="brand">kucrud template</span>
        <nav>
          <Link to="/products">Products</Link>
          <Link to="/login">Login</Link>
        </nav>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<Products />} />
          <Route path="/products" element={<Products />} />
          <Route path="/login" element={<Login />} />
        </Routes>
      </main>
    </div>
  )
}
