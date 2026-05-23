import './globals.css';
import BottomNav from '../components/BottomNav';

export const metadata = {
  title: 'FotMob Amharic',
  description: 'Football scores in Amharic',
};

export default function RootLayout({ children }) {
  return (
    <html lang="am">
      <body className="bg-gray-50 pb-20">
        <header className="bg-green-700 text-white p-4 sticky top-0 z-10 shadow-md">
          <h1 className="text-xl font-bold">ሶከር ኢትዮጵያ (FotMob)</h1>
        </header>
        <main className="max-w-md mx-auto bg-white min-h-screen shadow-sm">
          {children}
        </main>
        <BottomNav />
      </body>
    </html>
  );
}
