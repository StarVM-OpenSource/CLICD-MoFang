import { FormEvent, useState } from 'react'
import { Lock, User } from 'lucide-react'
import AppIcon from '../components/AppIcon'
import { useAuth } from '../contexts/AuthContext'

export default function Login() {
  const { login, accessCodeLogin } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // Check for access code in URL
  const urlParams = new URLSearchParams(window.location.search)
  const accessCode = urlParams.get('code') || ''

  const isAccessCodeLogin = !!accessCode

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setLoading(true)

    try {
      if (isAccessCodeLogin) {
        await accessCodeLogin(accessCode, password)
      } else {
        await login(username, password)
      }
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      setError(error.response?.data?.message || '登录失败，请检查用户名和密码')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md">
        <div className="bg-white rounded-lg border border-gray-200 shadow-sm p-8">
            <div className="flex flex-col items-center mb-8">
              <div className="w-16 h-16 flex items-center justify-center mb-4">
                <AppIcon className="w-10 h-10" />
              </div>
              <h1 className="text-2xl font-bold text-gray-950">CLICD</h1>
              <p className="text-gray-500 mt-1 text-sm">{isAccessCodeLogin ? '容器管理登录' : 'LXC Container Manager'}</p>
            </div>

            <form onSubmit={handleSubmit} className="space-y-5">
              {error && (
                <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-md text-sm">
                  {error}
                </div>
              )}

              {!isAccessCodeLogin && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1.5">
                    用户名
                  </label>
                  <div className="relative">
                    <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                      <User className="h-4 w-4 text-gray-400" />
                    </div>
                    <input
                      type="text"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      className="block w-full pl-10 pr-3 py-2.5 border border-gray-300 rounded-md text-black bg-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-black focus:border-black text-sm"
                      placeholder="输入用户名"
                      required
                      autoComplete="username"
                    />
                  </div>
                </div>
              )}

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1.5">
                密码
              </label>
              <div className="relative">
                <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                  <Lock className="h-4 w-4 text-gray-400" />
                </div>
                <input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  className="block w-full pl-10 pr-3 py-2.5 border border-gray-300 rounded-md text-black bg-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-black focus:border-black text-sm"
                  placeholder="输入密码"
                  required
                  autoComplete="current-password"
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full bg-black text-white py-2.5 rounded-md hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-black focus:ring-offset-2 transition-colors disabled:opacity-50 disabled:cursor-not-allowed text-sm font-medium"
            >
              {loading ? '登录中...' : '登录'}
            </button>
          </form>
        </div>

        <p className="text-center text-xs text-gray-400 mt-6">CLICD v1.1.3</p>
      </div>
    </div>
  )
}
